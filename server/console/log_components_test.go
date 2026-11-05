package console

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/logging"
)

// TestComponentsGetTheirLoggers checks that the output of the console logs as
// the commands component: with it at info and the other components at error,
// the output of a command shows, as the commands component.
func TestComponentsGetTheirLoggers(t *testing.T) {
	var out bytes.Buffer
	set := logging.New(logging.Options{Format: logging.JSON, Output: &out, Level: slog.LevelError,
		Levels: map[string]slog.Level{logging.Commands: slog.LevelInfo}})
	c := New(nil, set.Logger(logging.Server))
	o := &cmd.Output{}
	o.Print("done")
	(&consoleSource{log: c.log}).SendCommandOutput(o)
	var rec struct{ Component, Msg string }
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &rec); err != nil {
		t.Fatalf("logged %q, want one JSON line: %v", out.String(), err)
	}
	if rec.Component != logging.Commands || rec.Msg != "done" {
		t.Fatalf("logged %+v, want the output as the %s component", rec, logging.Commands)
	}
}
