package entity_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// caveTestWorld returns a world with a room of stone around the origin,
// eleven blocks wide, its floor at y 63 and its ceiling at y 72.
func caveTestWorld(t *testing.T) *world.World {
	t.Helper()
	w := newDeathTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		for x := -6; x <= 6; x++ {
			for z := -6; z <= 6; z++ {
				for y := 63; y <= 72; y++ {
					if y == 63 || y == 72 || x == -6 || x == 6 || z == -6 || z == 6 {
						tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
					}
				}
			}
		}
	})
	return w
}

func batOf(tx *world.Tx, h *world.EntityHandle) *entity.Bat {
	e, _ := h.Entity(tx)
	return e.(*entity.Bat)
}

// TestBatsHangFromCeilings checks Bat.tick and customServerAiStep: a bat
// resting under a solid block hangs still from the top of its block while
// no player comes near.
func TestBatsHangFromCeilings(t *testing.T) {
	w := caveTestWorld(t)
	var bat *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		bat = tx.AddEntity(entity.NewBat(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 71.3, .5}})).H()
	})
	for range 200 {
		stepWorld(t, w, 1)
		<-w.Exec(func(tx *world.Tx) {
			b := batOf(tx, bat)
			if !b.Resting() {
				t.Fatal("bat under the ceiling woke up with nobody near")
			}
			if y := b.Position()[1]; math.Abs(y-71.1) > 1e-9 {
				t.Fatalf("resting bat hangs at y %.3f, want 71.1, its top against the ceiling", y)
			}
		})
	}
}

// TestBatsWakeWhenPlayersComeNear checks Bat.customServerAiStep: a player
// within four blocks the bat sees wakes it, and it flies off.
func TestBatsWakeWhenPlayersComeNear(t *testing.T) {
	w := caveTestWorld(t)
	var bat *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		bat = tx.AddEntity(entity.NewBat(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 71.1, .5}})).H()
		addSurvivalPlayer(tx, mgl64.Vec3{3.5, 69, .5})
	})
	stepWorld(t, w, 4)
	start := aiEntityPos(w, bat)
	<-w.Exec(func(tx *world.Tx) {
		if batOf(tx, bat).Resting() {
			t.Fatal("bat kept resting with a player three blocks away")
		}
	})
	stepWorld(t, w, 100)
	if moved := aiEntityPos(w, bat).Sub(start).Len(); moved < 2 {
		t.Fatalf("woken bat flew %.2f blocks in five seconds", moved)
	}
}

// TestBatsFlyAbout checks Bat.customServerAiStep: a flying bat keeps
// flying about the room, steering up as much as gravity pulls it down, and
// spends little time on the floor.
func TestBatsFlyAbout(t *testing.T) {
	w := caveTestWorld(t)
	var bat *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		bat = tx.AddEntity(entity.NewBat(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 67, .5}})).H()
		batOf(tx, bat).SetResting(false)
	})
	var path float64
	last, grounded := aiEntityPos(w, bat), 0
	for range 400 {
		stepWorld(t, w, 1)
		<-w.Exec(func(tx *world.Tx) {
			b := batOf(tx, bat)
			if b.OnGround() {
				grounded++
			}
			path += b.Position().Sub(last).Len()
			last = b.Position()
		})
	}
	if path < 20 {
		t.Fatalf("flying bat flew %.2f blocks in 20 seconds, want at least 20", path)
	}
	if grounded > 100 {
		t.Fatalf("flying bat spent %d of 400 ticks on the floor", grounded)
	}
}

// TestHurtBatsWake checks Bat.hurtServer: hurting a resting bat wakes it.
func TestHurtBatsWake(t *testing.T) {
	w := caveTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		b := batOf(tx, tx.AddEntity(entity.NewBat(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 71.1, .5}})).H())
		b.Hurt(1, entity.AttackDamageSource{Attacker: addSurvivalPlayer(tx, mgl64.Vec3{.5, 64, 4.5})})
		if b.Resting() {
			t.Fatal("hurt bat kept resting")
		}
	})
}
