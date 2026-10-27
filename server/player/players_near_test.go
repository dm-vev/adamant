package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestMovedPlayersFoundWhereTheyAre checks a player teleported or moved is
// found near where it went at once, as mobs looking for players near them
// in the same tick must find it.
func TestMovedPlayersFoundWhereTheyAre(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		p := tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{0, 64, 0}}.New(Type, Config{Name: "Mover", GameMode: world.GameModeCreative})).(*Player)
		found := func(pos mgl64.Vec3) bool {
			for e := range tx.PlayersNear(pos, 8) {
				if e.H() == p.H() {
					return true
				}
			}
			return false
		}
		if !found(mgl64.Vec3{0, 64, 0}) {
			t.Fatal("player not found where it was added")
		}
		p.Teleport(mgl64.Vec3{900, 64, 900})
		if found(mgl64.Vec3{0, 64, 0}) || !found(mgl64.Vec3{900, 64, 900}) {
			t.Fatal("teleported player not found where it went")
		}
		p.Move(mgl64.Vec3{-200, 0, 0}, 0, 0)
		if found(mgl64.Vec3{900, 64, 900}) || !found(mgl64.Vec3{700, 64, 900}) {
			t.Fatal("moved player not found where it went")
		}
	})
}
