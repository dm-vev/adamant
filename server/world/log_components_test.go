package world

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/logging"
)

// TestComponentsGetTheirLoggers checks that a world logs as the world
// component, the backlog of its generator as the generator component and
// the close of its provider as the save component: with the level of one at
// debug and the rest at error, only its lines show.
func TestComponentsGetTheirLoggers(t *testing.T) {
	for _, component := range []string{logging.World, logging.Generator, logging.Save} {
		t.Run(component, func(t *testing.T) {
			var out bytes.Buffer
			set := logging.New(logging.Options{Format: logging.JSON, Output: &out, Level: slog.LevelError,
				Levels: map[string]slog.Level{component: slog.LevelDebug}})
			w := Config{Synchronous: true, Log: set.Logger(logging.Server)}.New()
			w.Log().Debug("a line of the world")
			w.handleGeneratorBackpressure(1)
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, line := range bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n")) {
				var rec struct{ Component string }
				if err := json.Unmarshal(line, &rec); err != nil {
					t.Fatalf("line %q is not JSON: %v", line, err)
				}
				got = append(got, rec.Component)
			}
			if len(got) == 0 || len(slices.Compact(got)) != 1 || got[0] != component {
				t.Fatalf("components logged with %s at debug = %v, want only %s", component, got, component)
			}
		})
	}
}
