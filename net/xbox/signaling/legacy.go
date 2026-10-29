package signaling

import (
	"context"
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

// LegacyURL is the service of the protocol the game spoke before JSON-RPC:
// a WebSocket of the network ID of the host, appended.
const LegacyURL = "wss://signal.franchise.minecraft-services.net/ws/v1.0/signaling/"

// Types of the messages of the legacy protocol.
const (
	legacyNotFound = iota
	legacySignal
	legacyCredentials
	legacyAccepted
	legacyAck
)

// legacyPing paces the pings of the legacy protocol, as the game sends them.
const legacyPing = 5 * time.Second

// LegacyConn is a connection to the signaling service over the protocol the
// game spoke before JSON-RPC, peers named by their NetherNet network IDs:
// a game published with it announces ConnectionType 3. It implements
// nethernet.Signaling as Conn does.
type LegacyConn struct {
	ws      *websocket.Conn
	signals chan nethernet.Signal
	done    chan struct{}
	err     error

	mu       sync.Mutex
	servers  []ICEServer
	gotCreds chan struct{}
	credOnce sync.Once

	writeMu   sync.Mutex
	closeOnce sync.Once
}

// legacyMessage is a message of the legacy protocol.
type legacyMessage struct {
	Type    int    `json:"Type"`
	From    string `json:"From,omitempty"`
	To      string `json:"To,omitempty"`
	Message string `json:"Message,omitempty"`
}

// DialLegacy connects to the service over the legacy protocol and waits for
// the TURN servers it hands out at once.
func DialLegacy(ctx context.Context, cfg Config) (*LegacyConn, error) {
	if cfg.Token == "" {
		return nil, errors.New("signaling: no token")
	}
	url := cfg.URL
	if url == "" {
		url = LegacyURL + strconv.FormatUint(cfg.NetworkID, 10)
	}
	dial := cfg.Dial
	if dial == nil {
		dial = func(ctx context.Context, url string, header http.Header) (*websocket.Conn, error) {
			c, _, err := websocket.Dial(ctx, url, header, nil)
			return c, err
		}
	}
	ws, err := dial(ctx, url, http.Header{"Authorization": {cfg.Token}, "User-Agent": {userAgent}})
	if err != nil {
		return nil, err
	}
	ws.SetReadLimit(readLimit)
	c := &LegacyConn{
		ws:       ws,
		signals:  make(chan nethernet.Signal, signalQueue),
		done:     make(chan struct{}),
		gotCreds: make(chan struct{}),
	}
	go c.read()
	go c.keepAlive()
	ctx, cancel := context.WithTimeout(ctx, credentialTimeout)
	defer cancel()
	select {
	case <-c.gotCreds:
		return c, nil
	case <-c.done:
		return nil, c.err
	case <-ctx.Done():
		_ = c.Close()
		return nil, fmt.Errorf("signaling: TURN credentials: %w", ctx.Err())
	}
}

// Credentials returns the STUN and TURN servers the service handed out.
func (c *LegacyConn) Credentials() []ICEServer {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ICEServer(nil), c.servers...)
}

// ReadSignal returns the next signal a peer sent, with its network ID.
func (c *LegacyConn) ReadSignal() (nethernet.Signal, error) {
	select {
	case s := <-c.signals:
		return s, nil
	case <-c.done:
		select {
		case s := <-c.signals:
			return s, nil
		default:
		}
		return nethernet.Signal{}, c.err
	}
}

// WriteSignal sends a signal to the network ID of its NetworkID.
func (c *LegacyConn) WriteSignal(s nethernet.Signal) error {
	return c.write(legacyMessage{Type: legacySignal, To: s.NetworkID, Message: s.String()})
}

func (c *LegacyConn) write(m legacyMessage) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.ws.WriteMessage(true, b)
}

// Close closes the connection.
func (c *LegacyConn) Close() error {
	c.end(errors.New("signaling: connection closed"))
	return nil
}

func (c *LegacyConn) end(err error) {
	c.closeOnce.Do(func() {
		c.err = err
		close(c.done)
		_ = c.ws.Close()
	})
}

// Done is closed once the connection ended.
func (c *LegacyConn) Done() <-chan struct{} { return c.done }

// Err returns why the connection ended, or nil while it lives.
func (c *LegacyConn) Err() error {
	select {
	case <-c.done:
		return c.err
	default:
		return nil
	}
}

func (c *LegacyConn) keepAlive() {
	t := time.NewTicker(legacyPing)
	defer t.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-t.C:
			if err := c.write(legacyMessage{Type: legacyNotFound}); err != nil {
				return
			}
		}
	}
}

// read handles what the service sends until the connection ends.
func (c *LegacyConn) read() {
	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			c.end(err)
			return
		}
		var m legacyMessage
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		switch m.Type {
		case legacySignal:
			s, err := nethernet.ParseSignal(m.Message)
			if err != nil || m.From == "" || len(m.From) > 20 {
				continue
			}
			s.NetworkID = m.From
			select {
			case c.signals <- s:
			default:
				// A reader far behind loses signals, as the service drops them.
			}
		case legacyCredentials:
			var creds struct {
				Upper []ICEServer `json:"TurnAuthServers"`
				Lower []ICEServer `json:"turnAuthServers"`
			}
			if json.Unmarshal([]byte(m.Message), &creds) != nil {
				continue
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
			c.credOnce.Do(func() { close(c.gotCreds) })
		}
	}
}
