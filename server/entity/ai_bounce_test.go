package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestMobsBounceOffSlime checks Entity.restituteMovementAfterCollisions: a
// cow dropped onto slime bounces back up, and one dropped onto stone does
// not.
func TestMobsBounceOffSlime(t *testing.T) {
	for _, c := range []struct {
		floor   world.Block
		bounces bool
	}{{block.Slime{}, true}, {block.Stone{}, false}} {
		w := wolfTestWorld(t)
		var cow *world.EntityHandle
		<-w.Exec(func(tx *world.Tx) {
			tx.SetBlock(cube.Pos{0, 63, 0}, c.floor, nil)
			cow = spawnAnimal(t, tx, "minecraft:cow", .5, nil)
			e, _ := cow.Entity(tx)
			e.(interface{ Teleport(mgl64.Vec3) }).Teleport(mgl64.Vec3{.5, 70, .5})
		})
		fell, bounced := false, false
		for range 60 {
			stepWorld(t, w, 1)
			<-w.Exec(func(tx *world.Tx) {
				e, _ := cow.Entity(tx)
				v := e.(interface{ Velocity() mgl64.Vec3 }).Velocity()[1]
				fell = fell || v < -.5
				bounced = bounced || fell && v > .3
			})
		}
		if bounced != c.bounces {
			t.Fatalf("cow dropped onto %T bounced %v, want %v", c.floor, bounced, c.bounces)
		}
	}
}
