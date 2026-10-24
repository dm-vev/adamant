package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func armadilloOf(tx *world.Tx, h *world.EntityHandle) *entity.Armadillo {
	e, _ := h.Entity(tx)
	return e.(*entity.Armadillo)
}

// waitArmadillo advances the world until cond holds or ticks run out.
func waitArmadillo(t *testing.T, w *world.World, h *world.EntityHandle, ticks int, cond func(*entity.Armadillo) bool) bool {
	t.Helper()
	for range ticks {
		stepWorld(t, w, 1)
		held := false
		<-w.Exec(func(tx *world.Tx) { held = cond(armadilloOf(tx, h)) })
		if held {
			return true
		}
	}
	return false
}

// TestArmadillosRollUpAtTheUndead checks the scare sensor and ArmadilloBallUp:
// an armadillo rolls up when a zombie comes within seven blocks, and once the
// zombie is gone it unrolls again.
func TestArmadillosRollUpAtTheUndead(t *testing.T) {
	w := wolfTestWorld(t)
	w.SetTime(18000)
	var armadillo, zombie *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		armadillo = tx.AddEntity(entity.NewArmadillo(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 64, .5}})).H()
		zombie = addMob(t, tx, "minecraft:zombie", mgl64.Vec3{4.5, 64, .5}).H()
	})
	if !waitArmadillo(t, w, armadillo, 100, func(a *entity.Armadillo) bool { return a.BedrockState() == "rolled_up" }) {
		t.Fatal("armadillo did not roll up with a zombie four blocks away")
	}
	<-w.Exec(func(tx *world.Tx) {
		if z, ok := zombie.Entity(tx); ok {
			_ = z.(interface{ CloseIn(*world.Tx) error }).CloseIn(tx)
		}
	})
	if !waitArmadillo(t, w, armadillo, 200, func(a *entity.Armadillo) bool { return !a.Rolling() }) {
		t.Fatal("armadillo did not unroll after the zombie was gone")
	}
}

// TestHitArmadillosRollUpAndShrugOffBlows checks Armadillo.actuallyHurt and
// hurtServer: a blow makes an armadillo roll up, after which blows hurt it
// half a point less than half.
func TestHitArmadillosRollUpAndShrugOffBlows(t *testing.T) {
	w := wolfTestWorld(t)
	var armadillo *world.EntityHandle
	var p world.Entity
	<-w.Exec(func(tx *world.Tx) {
		armadillo = tx.AddEntity(entity.NewArmadillo(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 64, .5}})).H()
		p = addSurvivalPlayer(tx, mgl64.Vec3{2.5, 64, .5})
	})
	stepWorld(t, w, 2)
	<-w.Exec(func(tx *world.Tx) {
		a := armadilloOf(tx, armadillo)
		a.Hurt(2, entity.AttackDamageSource{Attacker: p})
		if !a.Rolling() {
			t.Fatal("hit armadillo did not roll up")
		}
	})
	stepWorld(t, w, 20)
	<-w.Exec(func(tx *world.Tx) {
		a := armadilloOf(tx, armadillo)
		before := a.Health()
		a.Hurt(5, entity.AttackDamageSource{Attacker: p})
		if got := before - a.Health(); got != 2 {
			t.Fatalf("rolled up armadillo took %.1f damage from a blow of 5, want 2", got)
		}
	})
}

// TestArmadillosFollowSpiderEyes checks FollowTemptation: an armadillo
// follows a player holding a spider eye.
func TestArmadillosFollowSpiderEyes(t *testing.T) {
	w := wolfTestWorld(t)
	var armadillo *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		armadillo = tx.AddEntity(entity.NewArmadillo(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 64, .5}})).H()
		p := addSurvivalPlayer(tx, mgl64.Vec3{8.5, 64, .5})
		p.SetHeldItems(item.NewStack(item.SpiderEye{}, 1), item.Stack{})
	})
	if !waitArmadillo(t, w, armadillo, 400, func(a *entity.Armadillo) bool { return a.Position()[0] > 5 }) {
		t.Fatal("armadillo did not follow a player holding a spider eye eight blocks away")
	}
}

// TestArmadillosStroll checks the idle activity of ArmadilloAi: an armadillo
// strolls about.
func TestArmadillosStroll(t *testing.T) {
	w := wolfTestWorld(t)
	var armadillo *world.EntityHandle
	start := mgl64.Vec3{.5, 64, .5}
	<-w.Exec(func(tx *world.Tx) {
		armadillo = tx.AddEntity(entity.NewArmadillo(world.EntitySpawnOpts{Position: start})).H()
	})
	if !waitArmadillo(t, w, armadillo, 1200, func(a *entity.Armadillo) bool { return horizontalDistance(a.Position(), start) > 2 }) {
		t.Fatal("armadillo did not stroll two blocks in a minute")
	}
}
