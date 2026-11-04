package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestPlacingABlockLeavesTheBoxesOfItsModel places stone as a player does.
// Models of full blocks share the boxes they return, which every collision
// with such a block reads: checking the entities in the way of the block
// must leave them spanning the block, or entities fall through all full
// blocks of the world. An entity in the way still keeps the block out.
func TestPlacingABlockLeavesTheBoxesOfItsModel(t *testing.T) {
	t.Parallel()
	w := entityItemWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{8, 63, 8}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{10, 63, 8}, block.Stone{}, nil)
		start := mgl64.Vec3{9.5, 64, 10.5}
		p := tx.AddEntity(world.EntitySpawnOpts{Position: start}.New(Type, Config{Position: start, GameMode: world.GameModeSurvival})).(*Player)
		p.SetHeldItems(item.NewStack(block.Stone{}, 2), item.Stack{})

		p.UseItemOnBlock(cube.Pos{8, 63, 8}, cube.FaceUp, mgl64.Vec3{0.5, 1, 0.5})
		if _, ok := tx.Block(cube.Pos{8, 64, 8}).(block.Stone); !ok {
			t.Fatalf("stone was not placed: %v", tx.Block(cube.Pos{8, 64, 8}))
		}
		if boxes := (block.Stone{}).Model().BBox(cube.Pos{}, tx); len(boxes) != 1 || boxes[0] != cube.Box(0, 0, 0, 1, 1, 1) {
			t.Fatalf("placing stone moved the boxes of its model to %v", boxes)
		}

		tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{10.5, 64, 8.5}}))
		p.UseItemOnBlock(cube.Pos{10, 63, 8}, cube.FaceUp, mgl64.Vec3{0.5, 1, 0.5})
		if _, ok := tx.Block(cube.Pos{10, 64, 8}).(block.Air); !ok {
			t.Fatalf("stone was placed inside a cow: %v", tx.Block(cube.Pos{10, 64, 8}))
		}
	})
}
