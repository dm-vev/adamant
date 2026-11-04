package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestGhastlingsTakeAdultControlsWhenGrown checks HappyGhast.ageBoundaryReached:
// a ghastling flies with its brain, and once grown up it floats with the
// goals and the careful GhastMoveControl of an adult.
func TestGhastlingsTakeAdultControlsWhenGrown(t *testing.T) {
	t.Parallel()
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

// TestCarefulGhastsKeepABlockFromBlocks checks the careful
// GhastMoveControl.canReach: a happy ghast pushes itself only towards a
// place where every block within a block of its box is passable, and a
// ghast that is not careful also towards one just above the ground.
func TestCarefulGhastsKeepABlockFromBlocks(t *testing.T) {
	t.Parallel()
	w := world.Config{Entities: DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		for x := -8; x <= 8; x++ {
			for z := -8; z <= 8; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		g := tx.AddEntity(NewHappyGhast(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 70, .5}})).(*HappyGhast)
		g.Tick(tx, 0)
		m := g.state().ai.Mob(g.Ent, g)
		// The ground is at y 64. The box of a ghast at y 64.5 grown by a
		// block reaches into the stone, and at y 65 it ends on top of it.
		for _, c := range []struct {
			y                   float64
			careful, notCareful bool
		}{{64.5, false, true}, {65, true, true}, {68, true, true}} {
			travel := mgl64.Vec3{0, c.y - g.Position()[1], 0}
			if got := ghastCanReach(m, travel, true); got != c.careful {
				t.Errorf("careful ghast can reach y %v: %v, want %v", c.y, got, c.careful)
			}
			if got := ghastCanReach(m, travel, false); got != c.notCareful {
				t.Errorf("ghast that is not careful can reach y %v: %v, want %v", c.y, got, c.notCareful)
			}
		}
	})
}
