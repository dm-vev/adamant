package javaadapter

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/logging"
)

// TestComponentsGetTheirLoggers checks that the listener of the adapter logs
// as the network component: with it at info and the other components at
// error, the line that tells Java clients see the server shows, as the
// network component.
func TestComponentsGetTheirLoggers(t *testing.T) {
	var out bytes.Buffer
	set := logging.New(logging.Options{Format: logging.JSON, Output: &out, Level: slog.LevelError,
		Levels: map[string]slog.Level{logging.Network: slog.LevelInfo}})
	l, err := Config{Address: "127.0.0.1:0"}.Listener(server.Config{Log: set.Logger(logging.Server)}, testHost{})
	if err != nil {
		t.Fatal(err)
	}
	_ = l.Close()
	var rec struct{ Component, Msg string }
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &rec); err != nil {
		t.Fatalf("logged %q, want one JSON line: %v", out.String(), err)
	}
	if rec.Component != logging.Network || rec.Msg != "Clients of Java Edition see the server." {
		t.Fatalf("logged %+v, want the line of the listener as the %s component", rec, logging.Network)
	}
}
