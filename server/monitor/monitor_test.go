package monitor

import (
	"bytes"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/world"
)

// newServer returns a listening server of the overworld alone, closed with
// the test.
func newServer(t *testing.T) *server.Server {
	t.Helper()
	conf := server.Config{
		Log:           slog.New(slog.DiscardHandler),
		DisableNether: true, DisableEnd: true,
		Generator: func(world.Dimension, int64) world.Generator { return world.NopGenerator{} },
	}
	srv := conf.New()
	srv.Listen()
	t.Cleanup(func() { _ = srv.Close() })
	return srv
}

// get serves a GET of path through h, with the token passed if not empty.
func get(h http.Handler, path, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// TestMonitoringAuth checks the paths of the monitor with a Token: the
// metrics and the profiles need it, the probes never do; and the probes
// before and once the server is set.
func TestMonitoringAuth(t *testing.T) {
	c := DefaultConfig()
	c.Monitoring.Token = "s3cret"
	m := c.Service(Options{Log: slog.New(slog.DiscardHandler)})
	h, paths := m.handler()
	if got := strings.Join(paths, " "); got != "/metrics /livez /readyz /healthz /debug/pprof/" {
		t.Errorf("paths %s", got)
	}
	for _, path := range []string{"/metrics", "/debug/pprof/", "/debug/pprof/cmdline"} {
		if code := get(h, path, "").Code; code != http.StatusUnauthorized {
			t.Errorf("%s without the token: %d", path, code)
		}
		if code := get(h, path, "wrong").Code; code != http.StatusUnauthorized {
			t.Errorf("%s with a wrong token: %d", path, code)
		}
		if code := get(h, path, "s3cret").Code; code != http.StatusOK {
			t.Errorf("%s with the token: %d", path, code)
		}
	}
	if w := get(h, "/livez", ""); w.Code != http.StatusOK || w.Body.String() != "ok\n" {
		t.Errorf("/livez before the server: %d %q", w.Code, w.Body.String())
	}
	if w := get(h, "/readyz", ""); w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "[-]server/listening starting") {
		t.Errorf("/readyz before the server: %d %q", w.Code, w.Body.String())
	}

	m.setServer(newServer(t))
	if w := get(h, "/readyz?verbose", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "[+]server/listening\n") {
		t.Errorf("/readyz of a server listening: %d %q", w.Code, w.Body.String())
	}
	if w := get(h, "/healthz", ""); w.Code != http.StatusOK {
		t.Errorf("/healthz: %d", w.Code)
	}
	if w := get(h, "/metrics", "s3cret"); !strings.Contains(w.Body.String(), "adamant_world_ticks_total{") || !strings.Contains(w.Body.String(), "go_goroutines ") {
		t.Errorf("/metrics lacks the server or the network:\n%s", w.Body.String())
	}

	c = DefaultConfig()
	c.Monitoring.Pprof, c.Monitoring.Metrics = false, false
	h, paths = c.Service(Options{Log: slog.New(slog.DiscardHandler)}).handler()
	if code := get(h, "/debug/pprof/", "").Code; code != http.StatusNotFound || len(paths) != 3 {
		t.Errorf("profiles turned off: %d, paths %v", code, paths)
	}
}

// TestMonitoringServes checks that the monitor serves on its address and
// logs so, and that a failure to listen is logged at error, once, and
// leaves the server running without it.
func TestMonitoringServes(t *testing.T) {
	var logged bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logged, nil))
	c := DefaultConfig()
	c.Monitoring.Address = "127.0.0.1:0"
	m := c.Service(Options{Log: log})
	addr := m.Serve()
	if addr == nil {
		t.Fatalf("not served: %s", logged.String())
	}
	t.Cleanup(func() { _ = m.Close() })
	if !strings.Contains(logged.String(), `level=INFO msg="Monitoring served." addr=`+addr.String()+` paths="/metrics /livez /readyz /healthz /debug/pprof/"`) {
		t.Errorf("logged %q", logged.String())
	}
	resp, err := http.Get("http://" + addr.String() + "/livez")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "ok\n" {
		t.Errorf("/livez: %d %q", resp.StatusCode, body)
	}

	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	logged.Reset()
	c.Monitoring.Address = taken.Addr().String()
	if addr := c.Service(Options{Log: log}).Serve(); addr != nil {
		t.Fatal("served on an address taken")
	}
	if n := strings.Count(logged.String(), "level=ERROR"); n != 1 || !strings.Contains(logged.String(), `msg="Monitoring not served: listen tcp `) {
		t.Errorf("logged %q, want one error", logged.String())
	}
}

// TestLoopback checks which addresses are of this machine alone.
func TestLoopback(t *testing.T) {
	for addr, want := range map[string]bool{
		"localhost:6060": true, "LOCALHOST:1": true, "127.0.0.1:6060": true, "127.1.2.3:1": true, "[::1]:6060": true,
		":6060": false, "0.0.0.0:6060": false, "[::]:6060": false, "10.0.0.1:6060": false, "example.com:6060": false, "nonsense": false,
	} {
		if got := loopback(addr); got != want {
			t.Errorf("loopback(%q) = %v", addr, got)
		}
	}
}
