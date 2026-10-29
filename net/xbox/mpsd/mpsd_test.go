package mpsd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/websocket"
)

// directory stands in for the session directory and real-time activity of
// Xbox Live.
type directory struct {
	mu       sync.Mutex
	conns    int
	rta      *websocket.Conn
	sessions map[string]map[string]any
	handles  []map[string]any
	puts     []http.Header
}

func (d *directory) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "XBL3.0 x=1;token" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.URL.Path == "/connect" {
		if r.Header.Get("Sec-Websocket-Protocol") != rtaSubprotocol {
			http.Error(w, "subprotocol", http.StatusBadRequest)
			return
		}
		ws, err := websocket.Upgrade(w, r)
		if err != nil {
			return
		}
		_, data, err := ws.ReadMessage()
		var sub []any
		if err != nil || json.Unmarshal(data, &sub) != nil || len(sub) != 3 || sub[2] != connectionsURI {
			_ = ws.Close()
			return
		}
		d.mu.Lock()
		d.conns++
		n := d.conns
		d.rta = ws
		d.mu.Unlock()
		_ = ws.WriteMessage(true, []byte(fmt.Sprintf(`[1,1,0,7,{"ConnectionId":"CONN-%d"}]`, n)))
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}
	if r.Header.Get("X-Xbl-Contract-Version") != contractVersion {
		http.Error(w, "contract", http.StatusBadRequest)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var v map[string]any
	if json.Unmarshal(body, &v) != nil {
		http.Error(w, "json", http.StatusBadRequest)
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/handles":
		d.handles = append(d.handles, v)
		w.WriteHeader(http.StatusCreated)
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/serviceconfigs/"+ServiceConfigID+"/sessionTemplates/"+TemplateName+"/sessions/"):
		if d.sessions == nil {
			d.sessions = map[string]map[string]any{}
		}
		d.sessions[r.URL.Path] = v
		d.puts = append(d.puts, r.Header.Clone())
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	default:
		http.NotFound(w, r)
	}
}

// TestPublish publishes a game: the session names the network and player
// messaging ID friends signal through and the member of the host is kept
// alive by the connection the real-time activity hands out, which Update
// renews once it dropped.
func TestPublish(t *testing.T) {
	d := &directory{}
	srv := httptest.NewServer(d)
	defer srv.Close()
	cfg := Config{
		Token:             func(context.Context) (string, string, error) { return "XBL3.0 x=1;token", "2535400000000001", nil },
		NetherNetID:       1234567,
		PlayerMessagingID: "pmid-1",
		Endpoints:         Endpoints{SessionDirectory: srv.URL, RTA: "ws" + strings.TrimPrefix(srv.URL, "http") + "/connect"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h, err := Publish(ctx, cfg, Status{HostName: "Adamant", WorldName: "world", MemberCount: 3, MaxMemberCount: 100, Protocol: 2193, Version: "1.26.52"})
	if err != nil {
		t.Fatal(err)
	}
	session := func() map[string]any {
		d.mu.Lock()
		defer d.mu.Unlock()
		for _, s := range d.sessions {
			return s
		}
		return nil
	}
	path := func(v any, keys ...string) any {
		for _, k := range keys {
			m, ok := v.(map[string]any)
			if !ok {
				return nil
			}
			v = m[k]
		}
		return v
	}
	s := session()
	conn := path(s, "properties", "custom", "SupportedConnections").([]any)[0]
	if path(conn, "ConnectionType") != float64(7) || path(conn, "NetherNetId") != float64(1234567) || path(conn, "PmsgId") != "pmid-1" {
		t.Fatalf("supported connection %v", conn)
	}
	if path(s, "members", "me", "properties", "system", "connection") != "CONN-1" || path(s, "properties", "custom", "MemberCount") != float64(3) {
		t.Fatalf("session %v", s)
	}
	d.mu.Lock()
	if len(d.handles) != 1 || path(d.handles[0], "sessionRef", "name") != h.Name() || path(d.handles[0], "type") != "activity" || d.puts[0].Get("If-None-Match") != "*" {
		t.Fatalf("handles %v, first put %v", d.handles, d.puts[0])
	}
	// The real-time activity drops.
	_ = d.rta.Close()
	d.mu.Unlock()
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.mu.Lock()
		dropped := h.rta == nil
		h.mu.Unlock()
		if dropped {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the host did not notice the real-time activity dropped")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := h.Update(ctx, Status{MemberCount: 4, MaxMemberCount: 100}); err != nil {
		t.Fatal(err)
	}
	s = session()
	if path(s, "members", "me", "properties", "system", "connection") != "CONN-2" || path(s, "properties", "custom", "MemberCount") != float64(4) {
		t.Fatalf("session after the update %v", s)
	}
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if me, ok := path(session(), "members").(map[string]any); !ok || me["me"] != nil {
		t.Fatalf("session after closing %v", session())
	}
}

func TestSubscribed(t *testing.T) {
	for _, msg := range []string{`[3,7,{}]`, `[1,2,0,7,{"ConnectionId":"x"}]`, `nope`, `[1]`} {
		if c, err := subscribed([]byte(msg)); c != "" || err != nil {
			t.Fatalf("%s: %q, %v", msg, c, err)
		}
	}
	for _, msg := range []string{`[1,1,5,"refused"]`, `[1,1,0,7,{}]`, `[1,1,0,7,{"ConnectionId":"` + strings.Repeat("x", 65) + `"}]`} {
		if _, err := subscribed([]byte(msg)); err == nil {
			t.Fatalf("%s: no error", msg)
		}
	}
}
