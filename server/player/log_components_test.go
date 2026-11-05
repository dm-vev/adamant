package player

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/logging"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// componentsLogged returns the component of each JSON line in out.
func componentsLogged(t *testing.T, out *bytes.Buffer) []string {
	t.Helper()
	var components []string
	for _, line := range bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var rec struct{ Component string }
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("line %q is not JSON: %v", line, err)
		}
		components = append(components, rec.Component)
	}
	return components
}

// TestComponentsGetTheirLoggers checks that the lines of a player log as the
// player component, and those of its anti-cheat as the anticheat component:
// with the level of one at debug and the rest at error, only its lines show.
func TestComponentsGetTheirLoggers(t *testing.T) {
	for _, component := range []string{logging.Player, logging.AntiCheat} {
		t.Run(component, func(t *testing.T) {
			var out bytes.Buffer
			set := logging.New(logging.Options{Format: logging.JSON, Output: &out, Level: slog.LevelError,
				Levels: map[string]slog.Level{component: slog.LevelDebug}})
			w, _ := clientTestWorld(t)
			conf := withPolicy(anticheat.MovedTooQuickly, func(p *anticheat.Policy) { p.Action = anticheat.Log })
			withCheckedPlayer(t, w, clienttest.New(), conf, mgl64.Vec3{0.5, 100, 0.5}, func(tx *world.Tx, p *Player) {
				// An anti-cheat line: a move too far for a tick.
				farMove(p)
				// A player line: an edit of a sign that is not there.
				if err := p.EditSign(cube.PosFromVec3(p.Position()), false, "", ""); err != nil {
					t.Fatalf("EditSign: %v", err)
				}
			}, func(c *Config) { c.Log = set.Logger(logging.Server) })
			got := componentsLogged(t, &out)
			if len(got) != 1 || !slices.Equal(got, []string{component}) {
				t.Fatalf("components logged with %s at debug = %v, want [%s] only", component, got, component)
			}
		})
	}
}
