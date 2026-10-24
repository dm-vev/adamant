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

// caveTestWorld returns a world with a cave around the origin, eleven blocks
// wide, its floor at y 63 and its ceiling at y 72, in stone deep enough
// that a bat finds no air outside it to fly towards.
func caveTestWorld(t *testing.T) *world.World {
	t.Helper()
	w := newDeathTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		for x := -14; x <= 14; x++ {
			for z := -14; z <= 14; z++ {
				for y := 60; y <= 76; y++ {
					if y <= 63 || y >= 72 || x <= -6 || x >= 6 || z <= -6 || z >= 6 {
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
		// The player stands on a pillar, close enough to the bat.
		tx.SetBlock(cube.Pos{2, 67, 0}, block.Stone{}, nil)
		addSurvivalPlayer(tx, mgl64.Vec3{2.5, 68, .5})
	})
	// A woken bat may hang again from the ceiling now and then, out of
	// reach of the player or only to be woken again.
	woke, path, last := false, 0.0, mgl64.Vec3{.5, 71.1, .5}
	for range 60 {
		stepWorld(t, w, 1)
		<-w.Exec(func(tx *world.Tx) {
			b := batOf(tx, bat)
			woke = woke || !b.Resting()
			path, last = path+b.Position().Sub(last).Len(), b.Position()
		})
	}
	if !woke {
		t.Fatal("bat kept resting with a player under four blocks away")
	}
	if path < .3 {
		t.Fatalf("woken bat flew %.2f blocks in three seconds", path)
	}
}

// TestBatsFlyAbout checks Bat.customServerAiStep: a flying bat keeps
// flying about the room. It climbs slower than it sinks, so in a low room
// it may spend a while near the floor, but not most of its time on it.
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
	if path < 10 {
		t.Fatalf("flying bat flew %.2f blocks in 20 seconds, want at least 10", path)
	}
	if grounded > 300 {
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
