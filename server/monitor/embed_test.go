package monitor_test

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/monitor"
	"github.com/df-mc/dragonfly/server/remoteadmin"
	"github.com/df-mc/dragonfly/server/world"
)

// TestEmbedderRestartsServices does what a program that embeds the server
// does: it builds a server.Config by hand, adds the monitor and the remote
// admin API as services, listens, asks both, closes, and does it all again
// on the same ports in the same process. Each serves while the server runs,
// its port is free once Close returned, and no goroutine is left behind.
func TestEmbedderRestartsServices(t *testing.T) {
	dir := t.TempDir()
	monAddr, adminAddr := freeAddr(t), freeAddr(t)
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: 10 * time.Second}
	// The first signal.Notify of a process starts the goroutine of package
	// signal, which runs for as long as the process does.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	signal.Stop(signals)
	base := runtime.NumGoroutine()

	for run := 1; run <= 2; run++ {
		mon := monitor.DefaultConfig()
		mon.Monitoring.Address = monAddr
		admin := remoteadmin.DefaultConfig()
		admin.RemoteAdmin.Enabled, admin.RemoteAdmin.Address = true, adminAddr
		log := slog.New(slog.DiscardHandler)

		conf := server.Config{
			Log:           log,
			DisableNether: true, DisableEnd: true,
			Generator: func(world.Dimension, int64) world.Generator { return world.NopGenerator{} },
			Services: []server.Service{
				mon.Service(monitor.Options{Log: log}),
				admin.Service(remoteadmin.ListenOptions{Dir: dir, Log: log}),
			},
		}
		srv := conf.New()
		srv.Listen()

		if code, _ := get(t, client, "http://"+monAddr+"/livez", ""); code != http.StatusOK {
			t.Errorf("run %d: /livez answered %d, want 200", run, code)
		}
		if code, body := get(t, client, "http://"+monAddr+"/metrics", ""); code != http.StatusOK || !strings.Contains(body, "adamant_world_ticks_total{") {
			t.Errorf("run %d: /metrics answered %d without the metrics of the server:\n%s", run, code, body)
		}
		token := ownerToken(t, filepath.Join(dir, remoteadmin.DefaultTokenFile))
		if code, _ := get(t, client, "http://"+adminAddr+"/v1/status", token); code != http.StatusOK {
			t.Errorf("run %d: /v1/status answered %d, want 200", run, code)
		}
		if err := srv.Close(); err != nil {
			t.Fatal(err)
		}
		for _, addr := range []string{monAddr, adminAddr} {
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				t.Fatalf("run %d: %s is not free once the server closed: %v", run, addr, err)
			}
			_ = ln.Close()
		}
		if n := settleGoroutines(base); n > base {
			var stacks strings.Builder
			_ = pprof.Lookup("goroutine").WriteTo(&stacks, 1)
			t.Fatalf("run %d: %d goroutines once the server closed, %d before it started:\n%s", run, n, base, stacks.String())
		}
	}
}

// TestServiceServesBeforeStart checks that a program that wants /livez to
// answer while the worlds load may Serve before it makes the server, and
// that the server's Listen then neither serves a second time nor logs the
// monitor again.
func TestServiceServesBeforeStart(t *testing.T) {
	var logged strings.Builder
	log := slog.New(slog.NewTextHandler(&logged, nil))
	c := monitor.DefaultConfig()
	c.Monitoring.Address = "127.0.0.1:0"
	svc := c.Service(monitor.Options{Log: log})
	addr := svc.Serve()
	if addr == nil {
		t.Fatalf("not served: %s", logged.String())
	}
	client := &http.Client{Timeout: 10 * time.Second}
	if code, _ := get(t, client, "http://"+addr.String()+"/readyz", ""); code != http.StatusServiceUnavailable {
		t.Errorf("/readyz before the server: %d, want 503", code)
	}

	conf := server.Config{
		Log:           log,
		DisableNether: true, DisableEnd: true,
		Generator: func(world.Dimension, int64) world.Generator { return world.NopGenerator{} },
		Services:  []server.Service{svc},
	}
	srv := conf.New()
	srv.Listen()
	defer srv.Close()
	if svc.Addr().String() != addr.String() {
		t.Errorf("Listen moved the monitor from %s to %s", addr, svc.Addr())
	}
	if n := strings.Count(logged.String(), "Monitoring served."); n != 1 {
		t.Errorf("logged %d times that the monitor serves:\n%s", n, logged.String())
	}
	if code, _ := get(t, client, "http://"+addr.String()+"/readyz", ""); code != http.StatusOK {
		t.Errorf("/readyz of a server listening: %d, want 200", code)
	}
}

// ownerToken returns the first token of the file of the tokens.
func ownerToken(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if f := strings.Fields(line); len(f) == 3 && f[0] == "owner" {
			return f[2]
		}
	}
	t.Fatalf("no owner token in %s", path)
	return ""
}

// freeAddr returns an address of loopback whose TCP port was free.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

// get asks GET of url, with the bearer token passed if any, and returns the
// status code and the body.
func get(t *testing.T, c *http.Client, url, token string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode, string(body)
}

// settleGoroutines returns the number of goroutines once it is no more
// than want, or, if it stays above, after 10 seconds: goroutines that were
// told to end may take a moment to.
func settleGoroutines(want int) int {
	deadline := time.Now().Add(10 * time.Second)
	for {
		n := runtime.NumGoroutine()
		if n <= want || time.Now().After(deadline) {
			return n
		}
		time.Sleep(10 * time.Millisecond)
	}
}
