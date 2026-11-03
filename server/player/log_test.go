package player

import (
	"log/slog"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestPlayerLogger checks that a player logs through the logger of its
// Config, slog.Default() if it has none, and keeps it in its Data, so that
// a player made again from its data logs where it did.
func TestPlayerLogger(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	own := slog.New(slog.DiscardHandler)
	<-w.Exec(func(tx *world.Tx) {
		pos := mgl64.Vec3{0.5, 64, 0.5}
		p := tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(Type, Config{Position: pos, Log: own})).(*Player)
		if p.log != own || p.Data().Log != own {
			t.Errorf("player logs through %p, keeps %p in its data; want %p", p.log, p.Data().Log, own)
		}
		def := tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(Type, Config{Position: pos})).(*Player)
		if def.log != slog.Default() {
			t.Errorf("player without a logger logs through %p, want slog.Default() %p", def.log, slog.Default())
		}
	})
}
