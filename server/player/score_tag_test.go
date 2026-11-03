package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/text"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestSetScoreTag checks that SetScoreTag stores a lone text.Text as its
// legacy string, codes and all, and formats other values as before.
func TestSetScoreTag(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		pos := mgl64.Vec3{0.5, 64, 0.5}
		p := tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(Type, Config{Position: pos})).(*Player)
		for _, c := range []struct {
			a    []any
			want string
		}{
			{[]any{"§s", 12, " hearts"}, "§s 12  hearts"},
			{[]any{"§cline\n"}, "§cline"},
			{[]any{text.ParseLegacy("§aGreen §lbold")}, "§aGreen §lbold"},
			{[]any{text.Colourf("<red>10</red> points")}, "§r§c10§r points"},
			{[]any{text.Literal("x"), 1}, "x 1"},
		} {
			p.SetScoreTag(c.a...)
			if got := p.ScoreTag(); got != c.want {
				t.Errorf("SetScoreTag(%#v): score tag %q, want %q", c.a, got, c.want)
			}
		}
	})
}
