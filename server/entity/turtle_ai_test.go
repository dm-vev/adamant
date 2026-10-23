package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestTurtlesHeadForWater checks TurtleGoToWaterGoal: a turtle on land
// crawls slowly towards the water nearby, to the edge of it.
func TestTurtlesHeadForWater(t *testing.T) {
	w := poolTestWorld(t)
	var turtle *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		turtle = tx.AddEntity(entity.NewTurtle(world.EntitySpawnOpts{Position: mgl64.Vec3{16.5, 64, .5}})).H()
	})
	for range 1200 {
		w.AdvanceTick()
		close := false
		<-w.Exec(func(tx *world.Tx) { close = entityPos(tx, turtle)[0] < 13 })
		if close {
			return
		}
	}
	t.Fatal("turtle five blocks from water did not crawl to it in a minute")
}

// TestTurtlesSwimAway checks TurtleTravelGoal: a turtle in water swims off
// on a long journey.
func TestTurtlesSwimAway(t *testing.T) {
	w := poolTestWorld(t)
	var turtle *world.EntityHandle
	start := mgl64.Vec3{.5, 61, .5}
	<-w.Exec(func(tx *world.Tx) {
		// A travelling turtle only swims where every chunk within 34
		// blocks is loaded.
		for x := -96; x <= 96; x += 16 {
			for z := -96; z <= 96; z += 16 {
				tx.SetBlock(cube.Pos{x, 0, z}, block.Stone{}, nil)
			}
		}
		turtle = tx.AddEntity(entity.NewTurtle(world.EntitySpawnOpts{Position: start})).H()
	})
	furthest := 0.0
	for range 600 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) { furthest = max(furthest, horizontalDistance(entityPos(tx, turtle), start)) })
	}
	if furthest < 4 {
		t.Fatalf("turtle in water swam no further than %.2f blocks in half a minute", furthest)
	}
}
