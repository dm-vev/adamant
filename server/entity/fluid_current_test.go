package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// fluidTestStream builds a stream of water running east from a source at x
// 0 over a stone floor at y 63.
func fluidTestStream(tx *world.Tx) {
	for x := -2; x <= 9; x++ {
		for z := -2; z <= 2; z++ {
			tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
		}
	}
	tx.SetLiquid(cube.Pos{0, 64, 0}, block.Water{Depth: 8, Still: true})
	for x := 1; x <= 7; x++ {
		tx.SetLiquid(cube.Pos{x, 64, 0}, block.Water{Depth: 8 - x})
	}
}

// TestFluidFlowRunsDownTheStream checks FlowingFluid.getFlow points from the
// source to where the water runs out, and falls along walls.
func TestFluidFlowRunsDownTheStream(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		fluidTestStream(tx)
		l, _ := tx.Liquid(cube.Pos{3, 64, 0})
		if flow := fluidFlow(tx, cube.Pos{3, 64, 0}, l); !flow.ApproxEqualThreshold(mgl64.Vec3{1, 0, 0}, 1e-9) {
			t.Fatalf("flow along the stream = %v, want east", flow)
		}
		src, _ := tx.Liquid(cube.Pos{0, 64, 0})
		if flow := fluidFlow(tx, cube.Pos{0, 64, 0}, src); flow[0] <= 0 {
			t.Fatalf("flow at the source = %v, want east", flow)
		}

		pos := cube.Pos{0, 70, 5}
		tx.SetBlock(pos.Side(cube.FaceNorth), block.Stone{}, nil)
		tx.SetLiquid(pos, block.Water{Depth: 8, Falling: true})
		falling, _ := tx.Liquid(pos)
		if flow := fluidFlow(tx, pos, falling); flow[1] > -.99 {
			t.Fatalf("flow of water falling along a wall = %v, want down", flow)
		}
	})
}

// TestItemsFloatInWater checks items rise to the surface of water as
// ItemEntity.setUnderwaterMovement makes them, instead of sinking.
func TestItemsFloatInWater(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		for x := -2; x <= 2; x++ {
			for z := -2; z <= 2; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
				for y := 64; y <= 67; y++ {
					if x == -2 || x == 2 || z == -2 || z == 2 {
						tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
					} else if y < 67 {
						tx.SetLiquid(cube.Pos{x, y, z}, block.Water{Depth: 8, Still: true})
					}
				}
			}
		}
		e := tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 64, .5}}, item.NewStack(item.Stick{}, 1))).(*Ent)
		for i := range int64(300) {
			e.Tick(tx, i)
		}
		if y := e.Position()[1]; y < 66.5 {
			t.Fatalf("item in water rose to y %v, want it at the surface at 67", y)
		}
	})
}

// TestCurrentsCarryItemsAndMobs checks flowing water pushes items and mobs
// down the stream, as Entity.updateFluidInteraction does, and leaves fish be.
func TestCurrentsCarryItemsAndMobs(t *testing.T) {
	w := world.Config{Entities: DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		fluidTestStream(tx)
		it := tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{1.5, 64, .5}}, item.NewStack(item.Stick{}, 1))).(*Ent)
		for i := range int64(20) {
			it.Tick(tx, i)
		}
		if x := it.Position()[0]; x < 2 {
			t.Fatalf("item in the stream moved to x %v, want it carried east", x)
		}

		cow := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{1.5, 64, .5}})).(*Cow)
		ai, _ := cow.Ent.mobAI()
		m := ai.Mob(cow.Ent, cow)
		ai.updateFluids(m)
		if v := cow.Velocity(); v[0] <= 0 || v[2] != 0 {
			t.Fatalf("cow in the stream got velocity %v, want a push east", v)
		}

		fish := tx.AddEntity(NewCod(world.EntitySpawnOpts{Position: mgl64.Vec3{1.5, 64, .5}})).(*Cod)
		fishAI, _ := fish.Ent.mobAI()
		fishAI.updateFluids(fishAI.Mob(fish.Ent, fish))
		if v := fish.Velocity(); v != (mgl64.Vec3{}) {
			t.Fatalf("cod in the stream got velocity %v, want none: fish are not pushed by fluids", v)
		}
	})
}

// TestExperienceOrbsRiseInWater checks orbs rise in water as
// ExperienceOrb.setUnderwaterMovement makes them.
func TestExperienceOrbsRiseInWater(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		for y := 64; y <= 68; y++ {
			tx.SetLiquid(cube.Pos{0, y, 0}, block.Water{Depth: 8, Still: true})
		}
		for x := -1; x <= 1; x++ {
			for z := -1; z <= 1; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		e := tx.AddEntity(NewExperienceOrb(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 64, .5}, Velocity: mgl64.Vec3{0, -.01, 0}}, 3)).(*Ent)
		for i := range int64(100) {
			e.Tick(tx, i)
		}
		if y := e.Position()[1]; y < 65 {
			t.Fatalf("orb in water is at y %v after 5 seconds, want it rising", y)
		}
	})
}
