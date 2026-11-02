package signaling

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/nethernet"
	"github.com/dm-vev/adamant/net/websocket"
)

// service is a signaling service in the way of the game's: it routes the
// messages players send each other by player messaging ID, taken from the
// token of each connection.
type service struct {
	mu         sync.Mutex
	players    map[string]*websocket.Conn
	deliveries []string // message IDs confirmed to their senders
	nextID     int
}

func newService(t *testing.T) (*service, string) {
	t.Helper()
	s := &service{players: map[string]*websocket.Conn{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "MCToken ")
		if !ok || r.Header.Get("User-Agent") != userAgent {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ws, err := websocket.Upgrade(w, r)
		if err != nil {
			return
		}
		pmid := token + "-pmid"
		s.mu.Lock()
		s.players[pmid] = ws
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			delete(s.players, pmid)
			s.mu.Unlock()
			_ = ws.Close()
		}()
		for {
			_, data, err := ws.ReadMessage()
			if err != nil {
				return
			}
			s.handle(ws, pmid, data)
		}
	}))
	t.Cleanup(srv.Close)
	return s, "ws" + strings.TrimPrefix(srv.URL, "http")
}

func (s *service) handle(ws *websocket.Conn, from string, data []byte) {
	var req request
	if json.Unmarshal(data, &req) != nil {
		return
	}
	reply := func(result string) {
		_ = ws.WriteMessage(true, []byte(`{"jsonrpc":"2.0","id":`+string(req.ID)+`,"result":`+result+`}`))
	}
	switch req.Method {
	case methodTurnAuth:
		reply(`{"TurnAuthServers":[{"Username":"u","Password":"p","Urls":["stun:stun.example:3478","turn:turn.example:3478"]}]}`)
	case methodPing:
		reply(`null`)
	case methodSend:
		var p struct {
			To        string `json:"toPlayerId"`
			MessageID string `json:"messageId"`
			Message   string `json:"message"`
		}
		_ = json.Unmarshal(req.Params, &p)
		var msg inner
		_ = json.Unmarshal([]byte(p.Message), &msg)
		s.mu.Lock()
		if msg.Method == innerDelivery {
			var d struct {
				MessageID string `json:"messageId"`
			}
			_ = json.Unmarshal(msg.Params, &d)
			s.deliveries = append(s.deliveries, d.MessageID)
		}
		to, ok := s.players[p.To]
		s.nextID++
		id := s.nextID
		s.mu.Unlock()
		if !ok {
			_ = ws.WriteMessage(true, []byte(`{"jsonrpc":"2.0","id":`+string(req.ID)+`,"error":{"code":-32000,"message":"Player not registered"}}`))
			return
		}
		batch, _ := json.Marshal([]incoming{{From: from, Message: p.Message, ID: p.MessageID}})
		_ = to.WriteMessage(true, []byte(`{"jsonrpc":"2.0","method":"`+methodReceive+`","id":"`+strconv.Itoa(id)+`","params":`+string(batch)+`}`))
		reply(`{}`)
	}
}

func dialService(t *testing.T, url, token string, network uint64) *Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Dial(ctx, Config{Token: "MCToken " + token, NetworkID: network, URL: url})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// TestJoinOverSignaling joins a NetherNet listener whose offers come over
// the service: the host confirms the offer to the client and answers it,
// and the connection opens.
func TestJoinOverSignaling(t *testing.T) {
	svc, url := newService(t)
	host := dialService(t, url, "host", 4242)
	if creds := host.Credentials(); len(creds) != 1 || creds[0].Username != "u" || len(creds[0].URLs) != 2 {
		t.Fatalf("credentials %+v", creds)
	}
	l, err := nethernet.ListenConfig{}.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() { _ = l.Serve(host) }()

	key, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	client := dialService(t, url, "client", 99)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cli, err := nethernet.Dialer{Key: key}.DialSignaling(ctx, client, "host-pmid")
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	srv, err := l.AcceptConn()
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if err := cli.Send([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	_ = srv.SetReadDeadline(time.Now().Add(5 * time.Second))
	if msg, err := srv.ReadMessage(); err != nil || string(msg) != "hello" {
		t.Fatalf("read %q, %v", msg, err)
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.deliveries) < 2 {
		t.Fatalf("%d messages confirmed, want the offer and the answer", len(svc.deliveries))
	}
}

// TestServiceGarbage feeds a host what a broken or hostile service might
// send: nothing of it becomes a signal and the connection stays up.
func TestServiceGarbage(t *testing.T) {
	svc, url := newService(t)
	host := dialService(t, url, "host", 1)
	svc.mu.Lock()
	ws := svc.players["host-pmid"]
	svc.mu.Unlock()
	big := make([]incoming, maxBatch+10)
	for i := range big {
		big[i] = incoming{From: "x", Message: `{"jsonrpc":"2.0","method":"` + innerWebRTC + `","params":{"message":"NOPE 1 x"}}`}
	}
	batch, _ := json.Marshal(big)
	for _, msg := range []string{
		`not json`,
		`{"jsonrpc":"2.0","method":"` + methodReceive + `","params":"string"}`,
		`{"jsonrpc":"2.0","method":"` + methodReceive + `","params":{"From":"","Message":"x"}}`,
		`{"jsonrpc":"2.0","method":"` + methodReceive + `","params":{"From":"a","Message":"{\"method\":\"` + innerWebRTC + `\",\"params\":7}"}}`,
		`{"jsonrpc":"2.0","method":"` + methodReceive + `","params":` + string(batch) + `}`,
		`{"jsonrpc":"2.0","id":"unknown","result":{}}`,
		`{"jsonrpc":"2.0","id":17,"error":{"message":"x"}}`,
	} {
		if err := ws.WriteMessage(true, []byte(msg)); err != nil {
			t.Fatal(err)
		}
	}
	// A real signal after the garbage still arrives.
	good, _ := json.Marshal(webRTCParams{NetherNetID: "2", Message: "CONNECTERROR 5 13"})
	innerMsg, _ := json.Marshal(inner{JSONRPC: "2.0", Method: innerWebRTC, Params: good})
	one, _ := json.Marshal(incoming{From: "peer", Message: string(innerMsg), ID: "m1"})
	_ = ws.WriteMessage(true, []byte(`{"jsonrpc":"2.0","method":"`+methodReceive+`","id":"9","params":`+string(one)+`}`))
	got := make(chan nethernet.Signal, 1)
	go func() {
		s, err := host.ReadSignal()
		if err == nil {
			got <- s
		}
	}()
	select {
	case s := <-got:
		if s.Type != nethernet.SignalError || s.ConnectionID != 5 || s.NetworkID != "peer" {
			t.Fatalf("signal %+v", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no signal after the garbage")
	}
	if err := host.Err(); err != nil {
		t.Fatalf("connection ended: %v", err)
	}
}

func TestDialRefused(t *testing.T) {
	_, url := newService(t)
	if _, err := Dial(context.Background(), Config{Token: "Bearer nope", URL: url}); err == nil {
		t.Fatal("dialed with a token the service refuses")
	}
}

// TestLegacy speaks the protocol before JSON-RPC with a service of a test:
// the credentials come first, signals carry the network IDs of their
// peers, and pings keep the connection.
func TestLegacy(t *testing.T) {
	got := make(chan legacyMessage, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/signaling/42" || r.Header.Get("Authorization") != "MCToken t" {
			http.Error(w, "bad", http.StatusUnauthorized)
			return
		}
		ws, err := websocket.Upgrade(w, r)
		if err != nil {
			return
		}
		defer ws.Close()
		creds, _ := json.Marshal(map[string]any{"TurnAuthServers": []map[string]any{{"Urls": []string{"turn:192.0.2.1:3478"}, "Username": "u", "Password": "p"}}})
		_ = ws.WriteMessage(true, []byte(`{"Type":2,"From":"Server","Message":`+strconv.Quote(string(creds))+`}`))
		_ = ws.WriteMessage(true, []byte(`{"Type":1,"From":"77","Message":"CONNECTREQUEST 5 v=0"}`))
		for {
			_, data, err := ws.ReadMessage()
			if err != nil {
				return
			}
			var m legacyMessage
			_ = json.Unmarshal(data, &m)
			got <- m
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := DialLegacy(ctx, Config{Token: "MCToken t", NetworkID: 42, URL: "ws" + strings.TrimPrefix(srv.URL, "http") + "/signaling/42"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if creds := c.Credentials(); len(creds) != 1 || creds[0].Username != "u" {
		t.Fatalf("credentials %+v", creds)
	}
	s, err := c.ReadSignal()
	if err != nil || s.Type != nethernet.SignalOffer || s.ConnectionID != 5 || s.NetworkID != "77" {
		t.Fatalf("signal %+v, %v", s, err)
	}
	if err := c.WriteSignal(nethernet.Signal{Type: nethernet.SignalAnswer, ConnectionID: 5, Data: "v=0", NetworkID: "77"}); err != nil {
		t.Fatal(err)
	}
	if m := <-got; m.Type != legacySignal || m.To != "77" || m.Message != "CONNECTRESPONSE 5 v=0" {
		t.Fatalf("sent %+v", m)
	}
}

// set sets a variable for the length of a test.
func set[T any](t *testing.T, p *T, v T) {
	old := *p
	*p = v
	t.Cleanup(func() { *p = old })
}

// blackHole is a signaling service that hands out TURN credentials and
// then goes silent: it neither reads what the host sends nor answers its
// pings, as a path that drops every packet looks to the host.
func blackHole(t *testing.T) string {
	t.Helper()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Upgrade(w, r)
		if err != nil {
			return
		}
		defer ws.Close()
		for {
			_, data, err := ws.ReadMessage()
			if err != nil {
				return
			}
			var req request
			if json.Unmarshal(data, &req) == nil && req.Method == methodTurnAuth {
				_ = ws.WriteMessage(true, []byte(`{"jsonrpc":"2.0","id":`+string(req.ID)+`,"result":{"TurnAuthServers":[]}}`))
				break
			}
		}
		<-release
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// TestSilentService keeps a connection to a service that answers its pings
// and one to a service gone silent: the first stays up past the time a
// silent service is given, the second ends.
func TestSilentService(t *testing.T) {
	set(t, &pingFirst, 20*time.Millisecond)
	set(t, &pingInterval, 200*time.Millisecond)
	_, url := newService(t)
	live := dialService(t, url, "host", 1)
	silent := dialService(t, blackHole(t), "host", 2)
	select {
	case <-silent.Done():
		if silent.Err() == nil {
			t.Fatal("ended without a reason")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the connection to a silent service is still up after 5s")
	}
	time.Sleep(1500 * time.Millisecond)
	if err := live.Err(); err != nil {
		t.Fatalf("the connection to a service answering pings ended: %v", err)
	}
}

// TestStuckWrite writes signals to a service that stopped reading them,
// over either protocol: once the connection takes no more, a write fails
// and ends it rather than blocking, and Err tells why it ended.
func TestStuckWrite(t *testing.T) {
	set(t, &writeTimeout, 200*time.Millisecond)
	for name, c := range map[string]interface {
		nethernet.Signaling
		Err() error
	}{
		"JSON-RPC": dialService(t, blackHole(t), "host", 1),
		"legacy":   dialLegacy(t, legacyService(t, false)),
	} {
		t.Run(name, func(t *testing.T) {
			failed := make(chan error, 1)
			go func() {
				s := nethernet.Signal{Type: nethernet.SignalCandidate, ConnectionID: 1, Data: strings.Repeat("a", 256<<10), NetworkID: "7"}
				for {
					if err := c.WriteSignal(s); err != nil {
						failed <- err
						return
					}
				}
			}()
			select {
			case <-failed:
			case <-time.After(5 * time.Second):
				t.Fatal("writes to a service not reading still block after 5s")
			}
			if c.Err() == nil {
				t.Fatal("a failed write left the connection up")
			}
		})
	}
}

// legacyService is a service of the legacy protocol that hands out TURN
// credentials and sends nothing more. While live it reads what the host
// sends, which answers WebSocket pings; otherwise it goes silent.
func legacyService(t *testing.T, live bool) string {
	t.Helper()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Upgrade(w, r)
		if err != nil {
			return
		}
		defer ws.Close()
		_ = ws.WriteMessage(true, []byte(`{"Type":2,"From":"Server","Message":"{\"TurnAuthServers\":[]}"}`))
		for live {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
		<-release
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func dialLegacy(t *testing.T, url string) *LegacyConn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := DialLegacy(ctx, Config{Token: "MCToken t", NetworkID: 1, URL: url})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// TestLegacySilentService does as TestSilentService over the legacy
// protocol, whose service answers no message of the host: a live service
// is told by its answers to WebSocket pings.
func TestLegacySilentService(t *testing.T) {
	set(t, &legacyPing, 100*time.Millisecond)
	live := dialLegacy(t, legacyService(t, true))
	silent := dialLegacy(t, legacyService(t, false))
	select {
	case <-silent.Done():
		if silent.Err() == nil {
			t.Fatal("ended without a reason")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the connection to a silent service is still up after 5s")
	}
	time.Sleep(1500 * time.Millisecond)
	if err := live.Err(); err != nil {
		t.Fatalf("the connection to a service answering pings ended: %v", err)
	}
}
