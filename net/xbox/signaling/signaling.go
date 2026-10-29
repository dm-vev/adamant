// Package signaling connects a NetherNet host to the signaling service of
// the Minecraft services, over which Xbox Live friends join its game: the
// JSON-RPC protocol the game speaks at
// wss://signal.franchise.minecraft-services.net/ws/v1.0/messaging/connect.
// A Conn implements nethernet.Signaling, so a nethernet.Listener serves the
// offers that arrive over it. What the service sends is trusted for
// nothing: messages are bounded in size and number, and signals are parsed
// as any other.
package signaling

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/dm-vev/adamant/net/nethernet"
	"github.com/dm-vev/adamant/net/websocket"
)

// DefaultURL is the signaling service of the game.
const DefaultURL = "wss://signal.franchise.minecraft-services.net/ws/v1.0/messaging/connect"

// Methods of the JSON-RPC protocol.
const (
	methodTurnAuth    = "Signaling_TurnAuth_v1_0"
	methodSend        = "Signaling_SendClientMessage_v1_0"
	methodReceive     = "Signaling_ReceiveMessage_v1_0"
	methodPing        = "System_Ping_v1_0"
	methodPong        = "System_Pong_v1_0"
	innerWebRTC       = "Signaling_WebRtc_v1_0"
	innerDelivery     = "Signaling_DeliveryNotification_V1_0"
	userAgent         = "libHttpClient/1.0.0.0"
	pingFirst         = 30 * time.Second
	pingInterval      = 50 * time.Second
	readLimit         = 256 << 10
	maxBatch          = 64
	signalQueue       = 256
	credentialTimeout = 10 * time.Second
)

// ICEServer is a STUN or TURN server the service hands out, with the
// credentials of its TURN servers.
type ICEServer struct {
	URLs       []string `json:"Urls"`
	Username   string   `json:"Username"`
	Credential string   `json:"Password"`
}

// Config configures a connection to the service.
type Config struct {
	// Token is the Authorization header of the Minecraft services:
	// "MCToken ...", issued for the account of the host.
	Token string
	// NetworkID is the NetherNet network ID of the host, which the signals
	// it sends name as their sender.
	NetworkID uint64
	// URL is the service. Empty means DefaultURL.
	URL string
	// Dial opens the WebSocket. Nil means websocket.Dial with the default
	// TLS configuration.
	Dial func(ctx context.Context, url string, header http.Header) (*websocket.Conn, error)
}

// Conn is a connection to the signaling service. It implements
// nethernet.Signaling: ReadSignal returns the signals peers send, with the
// player messaging ID they came from as their NetworkID, and WriteSignal
// sends one to the player messaging ID of its NetworkID.
type Conn struct {
	ws        *websocket.Conn
	networkID string

	signals chan nethernet.Signal
	done    chan struct{}
	err     error // set before done closes

	mu       sync.Mutex
	pending  map[string]chan response
	servers  []ICEServer
	gotCreds chan struct{}

	closeOnce sync.Once
}

// request is a JSON-RPC request or notification.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      json.RawMessage `json:"id,omitempty"`
}

// response is a JSON-RPC response.
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("signaling: %s (%d)", e.Message, e.Code) }

// incoming is a message another player sent the host.
type incoming struct {
	From    string `json:"From"`
	Message string `json:"Message"`
	ID      string `json:"Id"`
}

// inner is the message one player sends another through the service.
type inner struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// webRTCParams are the params of a signal.
type webRTCParams struct {
	NetherNetID string `json:"netherNetId"`
	Message     string `json:"message"`
}

// Dial connects to the service and asks it for the TURN servers peers may
// relay through, which Credentials returns.
func Dial(ctx context.Context, cfg Config) (*Conn, error) {
	if cfg.Token == "" {
		return nil, errors.New("signaling: no token")
	}
	url := cfg.URL
	if url == "" {
		url = DefaultURL
	}
	header := http.Header{
		"Authorization": {cfg.Token},
		"User-Agent":    {userAgent},
		"Session-Id":    {newID()},
		"Request-Id":    {newID()},
	}
	dial := cfg.Dial
	if dial == nil {
		dial = func(ctx context.Context, url string, header http.Header) (*websocket.Conn, error) {
			c, _, err := websocket.Dial(ctx, url, header, nil)
			return c, err
		}
	}
	ws, err := dial(ctx, url, header)
	if err != nil {
		return nil, err
	}
	ws.SetReadLimit(readLimit)
	c := &Conn{
		ws:        ws,
		networkID: strconv.FormatUint(cfg.NetworkID, 10),
		signals:   make(chan nethernet.Signal, signalQueue),
		done:      make(chan struct{}),
		pending:   map[string]chan response{},
		gotCreds:  make(chan struct{}),
	}
	go c.read()
	go c.keepAlive()
	if err := c.fetchCredentials(ctx); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

// fetchCredentials asks for the TURN servers and waits for them.
func (c *Conn) fetchCredentials(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, credentialTimeout)
	defer cancel()
	result, err := c.call(ctx, methodTurnAuth, json.RawMessage(`{}`))
	if err != nil {
		return fmt.Errorf("signaling: TURN credentials: %w", err)
	}
	var creds struct {
		Upper []ICEServer `json:"TurnAuthServers"`
		Lower []ICEServer `json:"turnAuthServers"`
	}
	if err := json.Unmarshal(result, &creds); err != nil {
		return fmt.Errorf("signaling: TURN credentials: %w", err)
	}
	servers := creds.Upper
	if len(servers) == 0 {
		servers = creds.Lower
	}
	if len(servers) > 16 {
		servers = servers[:16]
	}
	c.mu.Lock()
	c.servers = servers
	c.mu.Unlock()
	return nil
}

// Credentials returns the STUN and TURN servers the service handed out.
func (c *Conn) Credentials() []ICEServer {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ICEServer(nil), c.servers...)
}

// ReadSignal returns the next signal a peer sent.
func (c *Conn) ReadSignal() (nethernet.Signal, error) {
	select {
	case s := <-c.signals:
		return s, nil
	case <-c.done:
		// Signals that arrived before the end are still returned.
		select {
		case s := <-c.signals:
			return s, nil
		default:
			return nethernet.Signal{}, c.err
		}
	}
}

// WriteSignal sends a signal to the player messaging ID of its NetworkID.
// It does not wait for the service to take it: an answer to a peer that
// left is lost, as it would be anyway.
func (c *Conn) WriteSignal(s nethernet.Signal) error {
	params, err := json.Marshal(webRTCParams{NetherNetID: c.networkID, Message: s.String()})
	if err != nil {
		return err
	}
	return c.send(s.NetworkID, innerWebRTC, params)
}

// send sends a message of a method to a player.
func (c *Conn) send(to, method string, params []byte) error {
	msg, err := json.Marshal(inner{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return err
	}
	p, err := json.Marshal(struct {
		To        string `json:"toPlayerId"`
		MessageID string `json:"messageId"`
		Message   string `json:"message"`
	}{to, newID(), string(msg)})
	if err != nil {
		return err
	}
	return c.notify(methodSend, p)
}

// Close closes the connection.
func (c *Conn) Close() error {
	c.end(errors.New("signaling: connection closed"))
	return nil
}

// end closes the connection for a reason, once.
func (c *Conn) end(err error) {
	c.closeOnce.Do(func() {
		c.err = err
		close(c.done)
		_ = c.ws.Close()
		c.mu.Lock()
		for id, ch := range c.pending {
			close(ch)
			delete(c.pending, id)
		}
		c.mu.Unlock()
	})
}

// Done is closed once the connection ended, and Err returns why.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Err returns why the connection ended, nil while it did not.
func (c *Conn) Err() error {
	select {
	case <-c.done:
		return c.err
	default:
		return nil
	}
}

// call sends a request and waits for its result.
func (c *Conn) call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	id := newID()
	ch := make(chan response, 1)
	c.mu.Lock()
	select {
	case <-c.done:
		c.mu.Unlock()
		return nil, c.err
	default:
	}
	c.pending[id] = ch
	c.mu.Unlock()
	if err := c.write(request{JSONRPC: "2.0", Method: method, Params: params, ID: json.RawMessage(strconv.Quote(id))}); err != nil {
		return nil, err
	}
	select {
	case r, ok := <-ch:
		if !ok {
			return nil, c.err
		}
		if r.Error != nil {
			return nil, r.Error
		}
		return r.Result, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

// notify sends a request whose response is not waited for.
func (c *Conn) notify(method string, params json.RawMessage) error {
	return c.write(request{JSONRPC: "2.0", Method: method, Params: params, ID: json.RawMessage(strconv.Quote(newID()))})
}

func (c *Conn) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := c.ws.WriteMessage(true, b); err != nil {
		c.end(err)
		return err
	}
	return nil
}

// keepAlive pings the service, which drops a host that goes quiet.
func (c *Conn) keepAlive() {
	t := time.NewTimer(pingFirst)
	defer t.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-t.C:
			if err := c.notify(methodPing, json.RawMessage(`{}`)); err != nil {
				return
			}
			t.Reset(pingInterval)
		}
	}
}

// read handles what the service sends until the connection ends.
func (c *Conn) read() {
	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			c.end(err)
			return
		}
		var msg struct {
			request
			Result json.RawMessage `json:"result"`
			Error  *rpcError       `json:"error"`
		}
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		switch {
		case msg.Method != "":
			c.handleRequest(msg.request)
		case len(msg.ID) > 0:
			c.handleResponse(response{ID: msg.ID, Result: msg.Result, Error: msg.Error})
		}
	}
}

// handleResponse hands a response to the call waiting for it.
func (c *Conn) handleResponse(r response) {
	id, err := strconv.Unquote(string(r.ID))
	if err != nil {
		return
	}
	c.mu.Lock()
	ch, ok := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	if ok {
		ch <- r
	}
}

// handleRequest answers a request of the service.
func (c *Conn) handleRequest(r request) {
	if len(r.ID) > 0 {
		// Every request of the service is answered with an empty result.
		_ = c.write(response{JSONRPC: "2.0", ID: r.ID, Result: json.RawMessage("null")})
	}
	if r.Method != methodReceive {
		return
	}
	var batch []incoming
	if len(r.Params) > 0 && r.Params[0] == '[' {
		if json.Unmarshal(r.Params, &batch) != nil {
			return
		}
	} else {
		var one incoming
		if json.Unmarshal(r.Params, &one) != nil {
			return
		}
		batch = []incoming{one}
	}
	if len(batch) > maxBatch {
		batch = batch[:maxBatch]
	}
	for _, m := range batch {
		c.receive(m)
	}
}

// receive confirms a message to its sender and passes the signal it holds
// on.
func (c *Conn) receive(m incoming) {
	if m.From == "" || len(m.From) > 128 {
		return
	}
	if m.ID != "" && len(m.ID) <= 128 {
		params, _ := json.Marshal(struct {
			MessageID string `json:"messageId"`
		}{m.ID})
		_ = c.send(m.From, innerDelivery, params)
	}
	var msg inner
	if json.Unmarshal([]byte(m.Message), &msg) != nil || msg.Method != innerWebRTC {
		return
	}
	var p webRTCParams
	if json.Unmarshal(msg.Params, &p) != nil {
		return
	}
	s, err := nethernet.ParseSignal(p.Message)
	if err != nil {
		return
	}
	s.NetworkID = m.From
	select {
	case c.signals <- s:
	case <-c.done:
	}
}

// newID returns a random ID in the form of a UUID.
func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
