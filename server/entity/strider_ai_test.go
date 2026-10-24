package entity_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// lavaTestWorld returns a world with a floor of stone at y 63 and a pool of
// lava depth blocks deep, seven by seven around the origin, its surface at
// y 63.
func lavaTestWorld(t *testing.T, depth int) *world.World {
	t.Helper()
	w := newDeathTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		for x := -20; x <= 20; x++ {
			for z := -20; z <= 20; z++ {
				for y := 63 - depth; y <= 63; y++ {
					tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
					if x >= -3 && x <= 3 && z >= -3 && z <= 3 && y > 63-depth {
						tx.SetBlock(cube.Pos{x, y, z}, block.Lava{Still: true, Depth: 8}, nil)
					}
				}
			}
		}
	})
	return w
}

func striderOf(tx *world.Tx, h *world.EntityHandle) *entity.Strider {
	e, _ := h.Entity(tx)
	return e.(*entity.Strider)
}

// TestStridersWalkOnLava checks Strider.canStandOnFluid, floatStrider and
// that striders are immune to fire: a strider stands on the surface of
// lava, half a block deep in it, without burning or shivering.
func TestStridersWalkOnLava(t *testing.T) {
	w := lavaTestWorld(t, 1)
	var strider *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		strider = tx.AddEntity(entity.NewStrider(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 64, .5}})).H()
	})
	for i := range 200 {
		stepWorld(t, w, 1)
		<-w.Exec(func(tx *world.Tx) {
			s := striderOf(tx, strider)
			if s.Health() < 20 || s.OnFireDuration() > 0 {
				t.Fatalf("strider on lava burnt, health %.1f", s.Health())
			}
			if i < 20 {
				return
			}
			if y := s.Position()[1]; math.Abs(y-63.5) > .01 {
				t.Fatalf("strider on lava stands at y %.3f, want 63.5, half a block into the lava", y)
			}
			if s.Suffocating() {
				t.Fatal("strider on lava shivers")
			}
		})
	}
}

// TestStridersFloatUpThroughLava checks Strider.floatStrider: a strider that
// sank into lava rises to its surface.
func TestStridersFloatUpThroughLava(t *testing.T) {
	w := lavaTestWorld(t, 3)
	var strider *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		strider = tx.AddEntity(entity.NewStrider(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 61, .5}})).H()
	})
	stepWorld(t, w, 300)
	if y := aiEntityPos(w, strider)[1]; math.Abs(y-63.5) > .01 {
		t.Fatalf("strider deep in lava came up to y %.3f, want 63.5 on its surface", y)
	}
}

// TestStridersHeadForLava checks StriderGoToLavaGoal and the suffocating of
// Strider.tick: a strider on stone shivers and walks into lava seven blocks
// away, where it stops shivering.
func TestStridersHeadForLava(t *testing.T) {
	w := lavaTestWorld(t, 1)
	var strider *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		strider = tx.AddEntity(entity.NewStrider(world.EntitySpawnOpts{Position: mgl64.Vec3{10.5, 64, .5}})).H()
	})
	stepWorld(t, w, 1)
	<-w.Exec(func(tx *world.Tx) {
		if !striderOf(tx, strider).Suffocating() {
			t.Fatal("strider on stone does not shiver")
		}
	})
	for range 1200 {
		stepWorld(t, w, 1)
		warm := false
		<-w.Exec(func(tx *world.Tx) {
			s := striderOf(tx, strider)
			warm = !s.Suffocating() && s.Position()[0] < 3.5
		})
		if warm {
			return
		}
	}
	t.Fatal("strider on stone did not walk into the lava nearby in a minute")
}

// TestWaterHurtsStriders checks LivingEntity.isSensitiveToWater for
// striders: water hurts a strider standing in it.
func TestWaterHurtsStriders(t *testing.T) {
	w := wolfTestWorld(t)
	var strider *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 64, 0}, block.Water{Still: true, Depth: 8}, nil)
		strider = tx.AddEntity(entity.NewStrider(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 64, .5}})).H()
	})
	stepWorld(t, w, 40)
	<-w.Exec(func(tx *world.Tx) {
		if h := striderOf(tx, strider).Health(); h >= 20 {
			t.Fatal("strider in water took no damage in two seconds")
		}
	})
}

// rideStrider spawns a saddled strider on stone and seats a survival player
// on it holding the item passed, looking towards positive x.
func rideStrider(t *testing.T, held item.Stack) (*world.World, *world.EntityHandle, *world.EntityHandle) {
	t.Helper()
	w := wolfTestWorld(t)
	var strider, rider *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		strider = spawnAnimal(t, tx, "minecraft:strider", -15.5, map[string]any{"Saddle": byte(1)})
		p := addSurvivalPlayer(tx, mgl64.Vec3{-15.5, 64, -1.5})
		p.SetHeldItems(held, item.Stack{})
		p.Move(mgl64.Vec3{}, -90-p.Rotation().Yaw(), -p.Rotation().Pitch())
		if !striderOf(tx, strider).Interact(tx, p, &item.UseContext{}) {
			t.Fatal("player could not ride the saddled strider")
		}
		rider = p.H()
	})
	return w, strider, rider
}

// TestStridersAreSteeredWithWarpedFungusOnSticks checks Strider.tickRidden
// and getRiddenSpeed: a strider walks the way the player riding it looks
// while the player holds a warped fungus on a stick, at 0.35 times its
// speed while it shivers, which takes a third off it.
func TestStridersAreSteeredWithWarpedFungusOnSticks(t *testing.T) {
	w, strider, _ := rideStrider(t, item.NewStack(item.WarpedFungusOnAStick{}, 1))
	start := aiEntityPos(w, strider)
	stepWorld(t, w, 40)
	moved := aiEntityPos(w, strider).Sub(start)
	if want := .175 * .66 * .35 / (1 - .6*.91) * 40 * .8; moved[0] < want || moved[0] > want*1.4 || math.Abs(moved[2]) > .5 {
		t.Fatalf("steered strider moved %.2f, want about %.2f blocks along positive x", moved, want/.8)
	}
}

// TestWarpedFungusOnSticksBoostStriders checks FoodOnAStickItem.use for
// striders: using a warped fungus on a stick boosts the strider once until
// the boost ends, at the cost of a point of durability, and a worn out one
// turns into a fishing rod.
func TestWarpedFungusOnSticksBoostStriders(t *testing.T) {
	w, strider, rider := rideStrider(t, item.NewStack(item.WarpedFungusOnAStick{}, 1))
	stepWorld(t, w, 20)
	use := func() (left item.Stack) {
		<-w.Exec(func(tx *world.Tx) {
			r, _ := rider.Entity(tx)
			p := r.(*player.Player)
			p.UseItem()
			left, _ = p.HeldItems()
		})
		return left
	}
	if d := use().Durability(); d != 99 {
		t.Fatalf("warped fungus on a stick has %d durability after boosting, want 99", d)
	}
	if d := use().Durability(); d != 99 {
		t.Fatalf("boosting a boosted strider took durability, %d left", d)
	}
	stepWorld(t, w, 70)
	start := aiEntityPos(w, strider)
	stepWorld(t, w, 40)
	if moved, want := aiEntityPos(w, strider).Sub(start), .175*.66*.35/(1-.6*.91)*40*1.15; moved[0] < want {
		t.Fatalf("boosted strider moved %.2f blocks in two seconds, want at least %.2f", moved[0], want)
	}

	w, _, rider = rideStrider(t, item.NewStack(item.WarpedFungusOnAStick{}, 1).Damage(99))
	if left := use(); left.Item() != (item.FishingRod{}) {
		t.Fatalf("worn out warped fungus on a stick turned into %v, want a fishing rod", left.Item())
	}
}
