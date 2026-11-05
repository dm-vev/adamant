package server_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/logging"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
)

// TestComponentsGetTheirLoggers makes a server whose logger writes JSON and
// closes it: with the level of one component at debug and the rest at error,
// only the lines of that component show, for the server, the saves of its
// checkpoints and database, and its worlds.
func TestComponentsGetTheirLoggers(t *testing.T) {
	for _, component := range []string{logging.Server, logging.Save, logging.World} {
		t.Run(component, func(t *testing.T) {
			var out bytes.Buffer
			set := logging.New(logging.Options{Format: logging.JSON, Output: &out, Level: slog.LevelError,
				Levels: map[string]slog.Level{component: slog.LevelDebug}})
			log := set.Logger(logging.Server)
			db, err := mcdb.Config{Log: log}.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			srv := server.Config{
				Log:           log,
				WorldProvider: db,
				DisableNether: true,
				DisableEnd:    true,
				Generator:     func(world.Dimension, int64) world.Generator { return world.NopGenerator{} },
				Listeners:     []func(server.Config, server.Host) (server.Listener, error){func(server.Config, server.Host) (server.Listener, error) { return clienttest.NewListener(), nil }},
			}.New()
			srv.Listen()
			// Closing saves a checkpoint and closes the worlds and their provider.
			srv.World().Log().Debug("a line of the world")
			if err := srv.Close(); err != nil {
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
				t.Fatalf("components logged with %s at debug = %v, want only %s:\n%s", component, got, component, &out)
			}
		})
	}
}
