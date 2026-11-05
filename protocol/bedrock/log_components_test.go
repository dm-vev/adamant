package bedrockadapter

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/versions"
	"github.com/df-mc/dragonfly/server/logging"
)

// TestComponentsGetTheirLoggers checks that the line that tells the
// protocols of the adapter logs as the network component: with it at info
// and the other components at error, it shows, as the network component.
func TestComponentsGetTheirLoggers(t *testing.T) {
	var out bytes.Buffer
	set := logging.New(logging.Options{Format: logging.JSON, Output: &out, Level: slog.LevelError,
		Levels: map[string]slog.Level{logging.Network: slog.LevelInfo}})
	uc := DefaultConfig()
	ps, primary, err := parseProtocols(uc.Network.Protocols, uc.Network.PrimaryProtocol, versions.Parse, versions.Configurable(), versions.NewestPrimary())
	if err != nil {
		t.Fatal(err)
	}
	logProtocols(set.Logger(logging.Server), ps, primary, nil)
	var rec struct{ Component, Msg string }
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &rec); err != nil {
		t.Fatalf("logged %q, want one JSON line: %v", out.String(), err)
	}
	if rec.Component != logging.Network || rec.Msg != "Protocols of Bedrock Edition." {
		t.Fatalf("logged %+v, want the protocols as the %s component", rec, logging.Network)
	}
}
