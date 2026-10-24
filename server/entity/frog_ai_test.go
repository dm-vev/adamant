package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// frogTestWorld returns a world with a floor of stone at y 63, built up by
// build, and a frog in it.
func frogTestWorld(t *testing.T, pos mgl64.Vec3, build func(tx *world.Tx)) (*world.World, *world.EntityHandle) {
	t.Helper()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	var frog *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		for x := -12; x <= 12; x++ {
			for z := -12; z <= 12; z++ {
				tx.SetBlock(cube.Pos{x, 62, z}, block.Stone{}, nil)
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		if build != nil {
			build(tx)
		}
		frog = tx.AddEntity(NewFrog(world.EntitySpawnOpts{Position: pos})).H()
	})
	return w, frog
}

func frogOf(tx *world.Tx, h *world.EntityHandle) *Frog {
	e, _ := h.Entity(tx)
	return e.(*Frog)
}

// waitFrog advances the world until cond holds or ticks run out.
func waitFrog(t *testing.T, w *world.World, h *world.EntityHandle, ticks int, cond func(tx *world.Tx, f *Frog) bool) bool {
	t.Helper()
	for range ticks {
		stepTicks(t, w, 1)
		held := false
		<-w.Exec(func(tx *world.Tx) { held = cond(tx, frogOf(tx, h)) })
		if held {
			return true
		}
	}
	return false
}

// TestFrogsLayFrogspawnBesideWater checks the LAY_SPAWN activity: a pregnant
// frog on land finds the water's edge and lays frogspawn on the water.
func TestFrogsLayFrogspawnBesideWater(t *testing.T) {
	w, frog := frogTestWorld(t, mgl64.Vec3{-3.5, 64, .5}, func(tx *world.Tx) {
		for x := 2; x <= 5; x++ {
			for z := -2; z <= 2; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Water{Still: true, Depth: 8}, nil)
			}
		}
	})
	<-w.Exec(func(tx *world.Tx) {
		f := frogOf(tx, frog)
		f.behaviour().br.set(memIsPregnant, true)
		// A frog that long jumps about takes longer to reach the water.
		f.behaviour().br.set(memLongJumpCooldownTicks, 100000)
	})
	laid := waitFrog(t, w, frog, 2400, func(tx *world.Tx, f *Frog) bool {
		for x := 2; x <= 5; x++ {
			for z := -2; z <= 2; z++ {
				if _, ok := tx.Block(cube.Pos{x, 64, z}).(block.FrogSpawn); ok {
					return true
				}
			}
		}
		return false
	})
	if !laid {
		t.Fatal("pregnant frog did not lay frogspawn on water six blocks away in two minutes")
	}
	<-w.Exec(func(tx *world.Tx) {
		if frogOf(tx, frog).Pregnant() {
			t.Fatal("frog was still pregnant after laying frogspawn")
		}
	})
}

// TestFrogsCroak checks Croak: a frog idling on land croaks now and then.
func TestFrogsCroak(t *testing.T) {
	w, frog := frogTestWorld(t, mgl64.Vec3{.5, 64, .5}, nil)
	if !waitFrog(t, w, frog, 2400, func(_ *world.Tx, f *Frog) bool { return f.Croaking() }) {
		t.Fatal("frog did not croak in two minutes")
	}
}

// TestFrogsLongJumpOutOfPits checks LongJumpToPreferredBlock: a frog in a
// pit too deep to climb out of leaps onto the ground around it.
func TestFrogsLongJumpOutOfPits(t *testing.T) {
	w, frog := frogTestWorld(t, mgl64.Vec3{.5, 64, .5}, func(tx *world.Tx) {
		for x := -6; x <= 6; x++ {
			for z := -6; z <= 6; z++ {
				if x < -1 || x > 1 || z < -1 || z > 1 {
					tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
					tx.SetBlock(cube.Pos{x, 65, z}, block.Stone{}, nil)
				}
			}
		}
	})
	stepTicks(t, w, 5)
	<-w.Exec(func(tx *world.Tx) { frogOf(tx, frog).behaviour().br.set(memLongJumpCooldownTicks, 1) })
	jumped := false
	out := waitFrog(t, w, frog, 2400, func(_ *world.Tx, f *Frog) bool {
		jumped = jumped || f.JumpGoalJumping()
		return f.Position()[1] >= 66 && f.OnGround()
	})
	if !out {
		t.Fatal("frog in a pit two blocks deep did not long jump out of it in two minutes")
	}
	if !jumped {
		t.Fatal("frog left its pit without a long jump")
	}
}

// TestFrogsSwimToLand checks the SWIM activity and TryFindLand: a frog in a
// pool swims to its shore.
func TestFrogsSwimToLand(t *testing.T) {
	w, frog := frogTestWorld(t, mgl64.Vec3{.5, 63.2, .5}, func(tx *world.Tx) {
		for x := -3; x <= 3; x++ {
			for z := -3; z <= 3; z++ {
				tx.SetBlock(cube.Pos{x, 62, z}, block.Water{Still: true, Depth: 8}, nil)
				tx.SetBlock(cube.Pos{x, 63, z}, block.Water{Still: true, Depth: 8}, nil)
			}
		}
	})
	if !waitFrog(t, w, frog, 1200, func(_ *world.Tx, f *Frog) bool {
		return f.OnGround() && f.Position()[1] >= 64 && f.behaviour().ai.waterHeight == 0
	}) {
		t.Fatal("frog in a pool did not swim to its shore in a minute")
	}
}

// TestFrogPregnancyIsSaved checks that a pregnant frog stays pregnant once
// saved and loaded, as Java keeps the is_pregnant memory of its brain.
func TestFrogPregnancyIsSaved(t *testing.T) {
	data := &world.EntityData{}
	frogConfig{pregnant: true}.Apply(data)
	nbt := FrogType.EncodeNBT(data)
	loaded := &world.EntityData{}
	FrogType.DecodeNBT(nbt, loaded)
	if !loaded.Data.(*frogBehaviour).br.has(memIsPregnant) {
		t.Fatal("pregnant frog was not pregnant once loaded")
	}
}
