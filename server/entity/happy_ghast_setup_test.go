package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestGhastlingsTakeAdultControlsWhenGrown checks HappyGhast.ageBoundaryReached:
// a ghastling flies with its brain, and once grown up it floats with the
// goals and the careful GhastMoveControl of an adult.
func TestGhastlingsTakeAdultControlsWhenGrown(t *testing.T) {
	w := world.Config{Entities: DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	var h *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		h = tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 70, .5}}.New(HappyGhastType, happyGhastConfig{age: -3})).H()
	})
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		e, _ := h.Entity(tx)
		b := e.(*HappyGhast).state()
		if _, flying := b.ai.moveOverride.(flyingMoveControl); !flying || len(b.ai.Goals.goals) != 0 {
			t.Fatalf("ghastling move control %T with %d goals, want a FlyingMoveControl and none", b.ai.moveOverride, len(b.ai.Goals.goals))
		}
	})
	for range 3 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		e, _ := h.Entity(tx)
		b := e.(*HappyGhast).state()
		if b.ai.moveOverride != &b.ghastMove || len(b.ai.Goals.goals) != 3 {
			t.Fatalf("grown happy ghast move control %T with %d goals, want its GhastMoveControl and three goals", b.ai.moveOverride, len(b.ai.Goals.goals))
		}
		if _, ok := b.ai.lookOverride.(happyGhastLookControl); !ok {
			t.Fatalf("grown happy ghast look control %T, want HappyGhastLookControl", b.ai.lookOverride)
		}
	})
}
