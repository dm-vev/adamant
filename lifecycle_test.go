package main

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
	"github.com/df-mc/dragonfly/server/logging"
	"github.com/df-mc/dragonfly/server/remoteadmin"
	"github.com/df-mc/dragonfly/server/world"
)

// TestServerRestartFreesServices starts and closes the server twice in one
// process, as a program that embeds it may, with its monitor and its
// remote admin API on the same ports both times: each serves while the
// server runs, its port is free once Server.Close returned, and no
// goroutine of the server, of its services or of CloseOnProgramEnd is left
// behind.
func TestServerRestartFreesServices(t *testing.T) {
	dir := t.TempDir()
	monAddr, remoteAddr := freeAddr(t), freeAddr(t)
	logs := logging.New(logging.Options{Format: logging.JSON, Output: io.Discard})
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: 10 * time.Second}
	// The first signal.Notify of a process starts the goroutine of package
	// signal, which runs for as long as the process does.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	signal.Stop(signals)
	base := runtime.NumGoroutine()

	for run := 1; run <= 2; run++ {
		proc := process{logs: logs, monitor: defaultMonitorConfig(), remote: remoteadmin.DefaultConfig(), adminDir: dir}
		proc.monitor.Monitoring.Address = monAddr
		proc.remote.RemoteAdmin.Enabled, proc.remote.RemoteAdmin.Address = true, remoteAddr
		srv := start(restartConfig(t, dir), proc)
		srv.CloseOnProgramEnd()

		if code := getCode(t, client, "http://"+monAddr+"/readyz", ""); code != http.StatusOK {
			t.Errorf("run %d: /readyz answered %d, want 200", run, code)
		}
		token := ownerToken(t, filepath.Join(dir, remoteadmin.DefaultTokenFile))
		if code := getCode(t, client, "http://"+remoteAddr+"/v1/status", token); code != http.StatusOK {
			t.Errorf("run %d: /v1/status answered %d, want 200", run, code)
		}
		if err := srv.Close(); err != nil {
			t.Fatal(err)
		}
		for _, addr := range []string{monAddr, remoteAddr} {
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

// restartConfig returns the Config of a server of no saved data whose lists
// are in dir.
func restartConfig(t *testing.T, dir string) server.Config {
	t.Helper()
	uc := server.DefaultConfig()
	uc.World.SaveData, uc.Players.SaveData = false, false
	uc.World.DisableNether, uc.World.DisableEnd = true, true
	uc.Whitelist.File = filepath.Join(dir, "white-list.txt")
	conf, err := uc.Config(slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	conf.Log = slog.New(slog.DiscardHandler)
	conf.Generator = func(world.Dimension, int64) world.Generator { return world.NopGenerator{} }
	return conf
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

// getCode asks GET of url, with the bearer token passed if any, and returns the
// status code.
func getCode(t *testing.T, c *http.Client, url, token string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode
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
