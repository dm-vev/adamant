package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func dolphinOf(tx *world.Tx, h *world.EntityHandle) *entity.Dolphin {
	e, ok := h.Entity(tx)
	if !ok {
		return nil
	}
	return e.(*entity.Dolphin)
}

// TestDolphinsSwimAround checks the RandomSwimmingGoal and
// SmoothSwimmingMoveControl of dolphins: a dolphin swims about its pool.
func TestDolphinsSwimAround(t *testing.T) {
	w := poolTestWorld(t)
	var dolphin *world.EntityHandle
	start := mgl64.Vec3{.5, 61, .5}
	<-w.Exec(func(tx *world.Tx) {
		dolphin = tx.AddEntity(entity.NewDolphin(world.EntitySpawnOpts{Position: start})).H()
		addSurvivalPlayer(tx, mgl64.Vec3{-15.5, 64, .5})
	})
	furthest := 0.0
	for range 600 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) { furthest = max(furthest, entityPos(tx, dolphin).Sub(start).Len()) })
	}
	if furthest < 4 {
		t.Fatalf("dolphin swam no further than %.2f blocks in half a minute", furthest)
	}
}

// TestDolphinsBreatheAir checks BreathAirGoal: a dolphin low on air swims up
// to the surface and breathes in.
func TestDolphinsBreatheAir(t *testing.T) {
	w := poolTestWorld(t)
	var dolphin *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		dolphin = tx.AddEntity(entity.NewDolphin(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 60, .5}})).H()
	})
	// Let the dolphin settle under water, then take its air.
	for range 5 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) { dolphinOf(tx, dolphin).SetAirSupply(100) })
	for range 200 {
		w.AdvanceTick()
		breathed := false
		<-w.Exec(func(tx *world.Tx) { breathed = dolphinOf(tx, dolphin).AirSupply() == 4800 })
		if breathed {
			return
		}
	}
	t.Fatal("dolphin low on air did not surface to breathe in ten seconds")
}

// TestDolphinsPlayWithItems checks Dolphin.MoveToItemGoal, pickUpItem and
// PlayWithItemsGoal: a dolphin swims to an item in the water, picks it up
// and tosses it away again, as a new item entity.
func TestDolphinsPlayWithItems(t *testing.T) {
	w := poolTestWorld(t)
	var original *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		// Walls keep the dolphin within eight blocks of the item, which
		// swimming about would take it beyond. They stand high enough out
		// of the water that the dolphin cannot leap over them.
		for x := -3; x <= 6; x++ {
			for z := -3; z <= 4; z++ {
				if x == -3 || x == 6 || z == -3 || z == 4 {
					for y := 60; y <= 68; y++ {
						tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
					}
				}
			}
		}
		tx.AddEntity(entity.NewDolphin(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 61, .5}}))
		original = tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{4.5, 61, .5}}, item.NewStack(item.Emerald{}, 2))).H()
	})
	for range 1200 {
		w.AdvanceTick()
		tossed := false
		<-w.Exec(func(tx *world.Tx) {
			if _, ok := original.Entity(tx); ok {
				return
			}
			for e := range tx.Entities() {
				if i, ok := e.(*entity.Ent); ok && i.H().Type() == entity.ItemType {
					tossed = true
				}
			}
		})
		if tossed {
			return
		}
	}
	t.Fatal("dolphin did not pick up and toss away an item in the water nearby in a minute")
}

// TestDolphinsDryOutOnLand checks Dolphin.tick: a dolphin on land hops about
// and dries out after two minutes.
func TestDolphinsDryOutOnLand(t *testing.T) {
	w := poolTestWorld(t)
	var dolphin *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		// A pen keeps the hopping dolphin from reaching the pool.
		for x := 14; x <= 19; x++ {
			for z := 14; z <= 19; z++ {
				if x == 14 || x == 19 || z == 14 || z == 19 {
					for y := 64; y <= 66; y++ {
						tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
					}
				}
			}
		}
		dolphin = tx.AddEntity(entity.NewDolphin(world.EntitySpawnOpts{Position: mgl64.Vec3{16.5, 64, 16.5}})).H()
	})
	hopped := false
	for i := range 2500 {
		w.AdvanceTick()
		health := 0.0
		<-w.Exec(func(tx *world.Tx) {
			d := dolphinOf(tx, dolphin)
			hopped = hopped || d.Velocity()[1] > .3
			health = d.Health()
		})
		if i < 2390 && health < 10 {
			t.Fatalf("dolphin on land dried out after %d ticks, want two minutes", i)
		}
		if health < 10 {
			if !hopped {
				t.Fatal("dolphin on land did not hop about")
			}
			return
		}
	}
	t.Fatal("dolphin on land did not dry out in two minutes")
}
