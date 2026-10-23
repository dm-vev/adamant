package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// poolTestWorld returns a pool of water from y 60 to 63 walled in from x and
// z -11 to 11, with stone around it at y 63 to stand on.
func poolTestWorld(t *testing.T) *world.World {
	t.Helper()
	w := newDeathTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		for x := -20; x <= 20; x++ {
			for z := -20; z <= 20; z++ {
				tx.SetBlock(cube.Pos{x, 59, z}, block.Stone{}, nil)
				inside := x >= -11 && x <= 11 && z >= -11 && z <= 11
				for y := 60; y <= 63; y++ {
					if inside {
						tx.SetBlock(cube.Pos{x, y, z}, block.Water{Still: true, Depth: 8}, nil)
					} else {
						tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
					}
				}
			}
		}
	})
	return w
}

func addFish(tx *world.Tx, pos mgl64.Vec3, id string) *world.EntityHandle {
	var h *world.EntityHandle
	opts := world.EntitySpawnOpts{Position: pos}
	switch id {
	case "minecraft:cod":
		h = entity.NewCod(opts)
	case "minecraft:pufferfish":
		h = entity.NewPufferfish(opts)
	default:
		h = entity.NewSalmon(opts)
	}
	return tx.AddEntity(h).H()
}

// TestFishSwimAround checks FishSwimGoal: a fish swims about the pool it is
// in.
func TestFishSwimAround(t *testing.T) {
	w := poolTestWorld(t)
	var fish *world.EntityHandle
	start := mgl64.Vec3{.5, 61, .5}
	<-w.Exec(func(tx *world.Tx) {
		fish = addFish(tx, start, "minecraft:cod")
		// Mobs idle without a player within 32 blocks.
		addSurvivalPlayer(tx, mgl64.Vec3{-15.5, 64, .5})
	})
	furthest := 0.0
	for range 600 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			pos := entityPos(tx, fish)
			if pos[1] > 64 || pos[1] < 60 {
				t.Fatalf("fish left the pool to %v", pos)
			}
			furthest = max(furthest, pos.Sub(start).Len())
		})
	}
	if furthest < 3 {
		t.Fatalf("fish swam no further than %.2f blocks in half a minute", furthest)
	}
}

// TestFishFleePlayers checks the AvoidEntityGoal of fish: a fish swims away
// from a player nearby.
func TestFishFleePlayers(t *testing.T) {
	w := poolTestWorld(t)
	var fish *world.EntityHandle
	player := mgl64.Vec3{.5, 61, .5}
	<-w.Exec(func(tx *world.Tx) {
		fish = addFish(tx, mgl64.Vec3{2.5, 61, .5}, "minecraft:cod")
		addSurvivalPlayer(tx, player)
	})
	for range 400 {
		w.AdvanceTick()
		far := false
		<-w.Exec(func(tx *world.Tx) { far = horizontalDistance(entityPos(tx, fish), player) > 6 })
		if far {
			return
		}
	}
	t.Fatal("fish did not swim away from a player two blocks away in 20 seconds")
}

// TestPufferfishPuffUpAndSting checks PufferfishPuffGoal and
// Pufferfish.playerTouch: a pufferfish puffs up by a player and poisons the
// player it touches.
func TestPufferfishPuffUpAndSting(t *testing.T) {
	w := poolTestWorld(t)
	var fish, player *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		// A cell two blocks long and wide keeps the pufferfish from swimming
		// away from the player.
		for x := -1; x <= 2; x++ {
			for z := -1; z <= 2; z++ {
				for y := 60; y <= 62; y++ {
					if x < 0 || x > 1 || z < 0 || z > 1 || y == 62 {
						tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
					}
				}
			}
		}
		p := addSurvivalPlayer(tx, mgl64.Vec3{.5, 60, .5})
		player = p.H()
		fish = addFish(tx, mgl64.Vec3{1.5, 60.2, 1.5}, "minecraft:pufferfish")
	})
	puffed, poisoned := false, false
	for range 200 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			if f, ok := fish.Entity(tx); ok {
				puffed = puffed || f.(*entity.Pufferfish).Variant() == 2
			}
			p, _ := player.Entity(tx)
			poisoned = poisoned || hasEffect(p.(entity.Living), effect.Poison)
		})
	}
	if !puffed {
		t.Fatal("pufferfish by a player did not puff up fully in ten seconds")
	}
	if !poisoned {
		t.Fatal("pufferfish did not poison the player it touched")
	}
}

// TestFishFlopAndDrownOnLand checks AbstractFish.aiStep and
// WaterAnimal.handleAirSupply: a fish on land flops about and starts to
// drown after 15 seconds.
func TestFishFlopAndDrownOnLand(t *testing.T) {
	w := poolTestWorld(t)
	var fish *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		fish = addFish(tx, mgl64.Vec3{15.5, 64, 15.5}, "minecraft:salmon")
	})
	flopped := false
	for i := range 340 {
		w.AdvanceTick()
		health := 0.0
		<-w.Exec(func(tx *world.Tx) {
			e, _ := fish.Entity(tx)
			s := e.(*entity.Salmon)
			flopped = flopped || s.Velocity()[1] > .2
			health = s.Health()
		})
		if i < 300 && health < 3 {
			t.Fatalf("fish on land took damage after %d ticks, want it to hold its breath for 15 seconds", i)
		}
		if health < 3 {
			if !flopped {
				t.Fatal("fish on land did not flop about")
			}
			return
		}
	}
	t.Fatal("fish on land did not drown in 17 seconds")
}
