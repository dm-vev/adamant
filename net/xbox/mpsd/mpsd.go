// Package mpsd publishes a NetherNet game on Xbox Live, as the game hosts
// one: a session of the MinecraftLobby template in the multiplayer session
// directory with an activity handle, which friends of the account see and
// join through, its member kept alive by a real-time activity connection.
// The session names the NetherNet network and player messaging ID friends
// signal through, with connection type 7, the JSON-RPC signaling.
package mpsd

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dm-vev/adamant/net/websocket"
)

// Constants of the sessions of Minecraft.
const (
	// ServiceConfigID is the service configuration of Minecraft.
	ServiceConfigID = "4fc10100-5f7a-4470-899b-280835760c07"
	// TemplateName is the template of the lobby sessions of Minecraft.
	TemplateName = "MinecraftLobby"
	// connectionTypeJSONRPC is the connection type of a session joined
	// through the JSON-RPC signaling.
	connectionTypeJSONRPC = 7
	// transportNetherNet is the transport layer of NetherNet.
	transportNetherNet = 2
	contractVersion    = "107"
	connectionsURI     = "https://sessiondirectory.xboxlive.com/connections/"
	rtaSubprotocol     = "rta.xboxlive.com.V2"
	maxResponse        = 1 << 20
)

// Endpoints are the services a Host talks to.
type Endpoints struct {
	SessionDirectory string
	RTA              string
}

// DefaultEndpoints are the services of Xbox Live.
var DefaultEndpoints = Endpoints{
	SessionDirectory: "https://sessiondirectory.xboxlive.com",
	RTA:              "wss://rta.xboxlive.com/connect",
}

// Status is what friends see of the game.
type Status struct {
	HostName       string
	WorldName      string
	GameMode       string // Survival, Creative or Adventure
	Version        string
	Protocol       int
	MemberCount    int
	MaxMemberCount int
}

// Config configures a Host.
type Config struct {
	// Token returns the XSTS token of Xbox Live as an Authorization header
	// ("XBL3.0 x=...") and the XUID of the account.
	Token func(ctx context.Context) (header, xuid string, err error)
	// NetherNetID is the network of the host.
	NetherNetID uint64
	// PlayerMessagingID is the ID the signaling service knows the host by.
	PlayerMessagingID string
	// Endpoints are the services. Zero values mean DefaultEndpoints.
	Endpoints Endpoints
	// HTTPClient sends requests. Nil means http.DefaultClient.
	HTTPClient *http.Client
	// Dial opens the real-time activity WebSocket. Nil means
	// websocket.Dial.
	Dial func(ctx context.Context, url string, header http.Header) (*websocket.Conn, error)
}

// Host is a published game.
type Host struct {
	cfg  Config
	name string

	mu         sync.Mutex
	rta        *websocket.Conn
	connection string
	status     Status
	closed     bool
}

// Publish creates the session of a game and its activity handle.
func Publish(ctx context.Context, cfg Config, status Status) (*Host, error) {
	if cfg.Token == nil || cfg.PlayerMessagingID == "" || cfg.NetherNetID == 0 {
		return nil, errors.New("mpsd: token, player messaging ID and NetherNet ID are needed")
	}
	if cfg.Endpoints.SessionDirectory == "" {
		cfg.Endpoints.SessionDirectory = DefaultEndpoints.SessionDirectory
	}
	if cfg.Endpoints.RTA == "" {
		cfg.Endpoints.RTA = DefaultEndpoints.RTA
	}
	h := &Host{cfg: cfg, name: strings.ToUpper(newUUID()), status: status}
	if err := h.connect(ctx); err != nil {
		return nil, err
	}
	if err := h.write(ctx, true); err != nil {
		h.closeRTA()
		return nil, err
	}
	if err := h.activity(ctx); err != nil {
		h.closeRTA()
		return nil, err
	}
	return h, nil
}

// Name returns the name of the session.
func (h *Host) Name() string { return h.name }

// Update tells friends a new status of the game, and brings the member of
// the host back to life over a new real-time activity connection if the
// last one dropped.
func (h *Host) Update(ctx context.Context, status Status) error {
	h.mu.Lock()
	h.status = status
	alive := h.rta != nil
	h.mu.Unlock()
	if !alive {
		if err := h.connect(ctx); err != nil {
			return err
		}
	}
	return h.write(ctx, false)
}

// Close removes the host from the session, which ends it, and closes the
// real-time activity connection.
func (h *Host) Close(ctx context.Context) error {
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
	err := h.request(ctx, http.MethodPut, h.sessionURL(), map[string]any{"members": map[string]any{"me": nil}}, nil, nil)
	h.closeRTA()
	return err
}

func (h *Host) closeRTA() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.rta != nil {
		_ = h.rta.Close()
		h.rta = nil
	}
}

// connect opens the real-time activity connection and subscribes to the
// connections of the session directory, which hands out the connection ID
// the member of the host is kept alive by.
func (h *Host) connect(ctx context.Context) error {
	header, _, err := h.cfg.Token(ctx)
	if err != nil {
		return err
	}
	dial := h.cfg.Dial
	if dial == nil {
		dial = func(ctx context.Context, url string, header http.Header) (*websocket.Conn, error) {
			c, _, err := websocket.Dial(ctx, url, header, nil)
			return c, err
		}
	}
	ws, err := dial(ctx, h.cfg.Endpoints.RTA, http.Header{
		"Authorization":          {header},
		"Sec-Websocket-Protocol": {rtaSubprotocol},
	})
	if err != nil {
		return fmt.Errorf("mpsd: real-time activity: %w", err)
	}
	ws.SetReadLimit(64 << 10)
	sub, _ := json.Marshal([]any{1, 1, connectionsURI})
	if err := ws.WriteMessage(true, sub); err != nil {
		_ = ws.Close()
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = ws.SetReadDeadline(time.Unix(1, 0)) })
	defer stop()
	_ = ws.SetReadDeadline(time.Now().Add(15 * time.Second))
	var connection string
	for connection == "" {
		_, data, err := ws.ReadMessage()
		if err != nil {
			_ = ws.Close()
			return fmt.Errorf("mpsd: subscribe: %w", err)
		}
		connection, err = subscribed(data)
		if err != nil {
			_ = ws.Close()
			return err
		}
	}
	_ = ws.SetReadDeadline(time.Time{})
	h.mu.Lock()
	if h.rta != nil {
		_ = h.rta.Close()
	}
	h.rta, h.connection = ws, connection
	h.mu.Unlock()
	go h.watch(ws)
	return nil
}

// subscribed returns the connection ID of the response to the subscription,
// or an empty string for another message.
func subscribed(data []byte) (string, error) {
	var msg []json.RawMessage
	if json.Unmarshal(data, &msg) != nil || len(msg) < 3 {
		return "", nil
	}
	var typ, seq, status int
	if json.Unmarshal(msg[0], &typ) != nil || typ != 1 || json.Unmarshal(msg[1], &seq) != nil || seq != 1 {
		return "", nil
	}
	if json.Unmarshal(msg[2], &status) != nil || status != 0 || len(msg) < 5 {
		return "", fmt.Errorf("mpsd: subscription refused with status %d", status)
	}
	var custom struct {
		ConnectionID string `json:"ConnectionId"`
	}
	if json.Unmarshal(msg[4], &custom) != nil || custom.ConnectionID == "" || len(custom.ConnectionID) > 64 {
		return "", errors.New("mpsd: subscription without a connection ID")
	}
	return custom.ConnectionID, nil
}

// watch reads the real-time activity connection until it drops, after
// which the next Update connects again.
func (h *Host) watch(ws *websocket.Conn) {
	for {
		if _, _, err := ws.ReadMessage(); err != nil {
			h.mu.Lock()
			if h.rta == ws {
				h.rta = nil
			}
			h.mu.Unlock()
			return
		}
	}
}

func (h *Host) sessionURL() string {
	return h.cfg.Endpoints.SessionDirectory + "/serviceconfigs/" + ServiceConfigID + "/sessionTemplates/" + TemplateName + "/sessions/" + h.name
}

// write puts the session: its properties and the member of the host.
func (h *Host) write(ctx context.Context, create bool) error {
	_, xuid, err := h.cfg.Token(ctx)
	if err != nil {
		return err
	}
	h.mu.Lock()
	status, connection := h.status, h.connection
	h.mu.Unlock()
	mode := status.GameMode
	if mode == "" {
		mode = "Survival"
	}
	body := map[string]any{
		"properties": map[string]any{
			"system": map[string]any{"joinRestriction": "followed", "readRestriction": "followed", "closed": false},
			"custom": map[string]any{
				"BroadcastSetting":        3,
				"CrossPlayDisabled":       false,
				"Joinability":             "joinable_by_friends",
				"LanGame":                 false,
				"MaxMemberCount":          status.MaxMemberCount,
				"MemberCount":             status.MemberCount,
				"OnlineCrossPlatformGame": true,
				"SupportedConnections": []map[string]any{{
					"ConnectionType": connectionTypeJSONRPC,
					"HostIpAddress":  "",
					"HostPort":       0,
					"NetherNetId":    h.cfg.NetherNetID,
					"PmsgId":         h.cfg.PlayerMessagingID,
				}},
				"TitleId":        0,
				"TransportLayer": transportNetherNet,
				"levelId":        "level",
				"hostName":       status.HostName,
				"ownerId":        xuid,
				"rakNetGUID":     "",
				"worldName":      status.WorldName,
				"worldType":      mode,
				"protocol":       status.Protocol,
				"version":        status.Version,
				"isEditorWorld":  false,
				"isHardcore":     false,
			},
		},
		"members": map[string]any{
			"me": map[string]any{
				"constants": map[string]any{"system": map[string]any{"xuid": xuid, "initialize": true}},
				"properties": map[string]any{"system": map[string]any{
					"active":       true,
					"connection":   connection,
					"subscription": map[string]any{"id": strings.ToUpper(newUUID()), "changeTypes": []string{"everything"}},
				}},
			},
		},
	}
	header := http.Header{}
	if create {
		header.Set("If-None-Match", "*")
	}
	return h.request(ctx, http.MethodPut, h.sessionURL(), body, header, nil)
}

// activity creates the activity handle of the session, which shows it to
// friends.
func (h *Host) activity(ctx context.Context) error {
	return h.request(ctx, http.MethodPost, h.cfg.Endpoints.SessionDirectory+"/handles", map[string]any{
		"version": 1,
		"type":    "activity",
		"sessionRef": map[string]any{
			"scid":         ServiceConfigID,
			"templateName": TemplateName,
			"name":         h.name,
		},
	}, nil, nil)
}

// request sends a JSON request to the session directory.
func (h *Host) request(ctx context.Context, method, url string, body any, header http.Header, out any) error {
	auth, _, err := h.cfg.Token(ctx)
	if err != nil {
		return err
	}
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Xbl-Contract-Version", contractVersion)
	client := h.cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		if len(data) > 256 {
			data = data[:256]
		}
		return fmt.Errorf("mpsd: %s %s: status %d: %s", method, req.URL.Path, resp.StatusCode, data)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// newUUID returns a random UUID.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	s := hex.EncodeToString(b[:])
	return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]
}
