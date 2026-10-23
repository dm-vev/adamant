package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func camelSitting(tx *world.Tx, h *world.EntityHandle) bool {
	e, _ := h.Entity(tx)
	return e.(interface{ Sitting() bool }).Sitting()
}

// TestCamelsSitDownAndStayPut checks CamelAi.RandomSitting and
// Camel.refuseToMove: an idle camel sits down now and then and does not move
// while it sits.
func TestCamelsSitDownAndStayPut(t *testing.T) {
	w := traderTestWorld(t)
	var h *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		h = spawnAnimal(t, tx, "minecraft:camel", .5, nil)
		addSurvivalPlayer(tx, mgl64.Vec3{.5, 64, 12.5})
	})
	sat := false
	for range 4000 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) { sat = camelSitting(tx, h) })
		if sat {
			break
		}
	}
	if !sat {
		t.Fatal("idle camel never sat down in three minutes")
	}
	var start mgl64.Vec3
	<-w.Exec(func(tx *world.Tx) { start = entityPos(tx, h) })
	for range 100 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			if camelSitting(tx, h) {
				if d := horizontalDistance(entityPos(tx, h), start); d > .05 {
					t.Fatalf("sitting camel moved %.2f blocks", d)
				}
			}
		})
	}
}

// TestCamelsFollowCactus checks FollowTemptation for camels: an adult camel
// follows a player holding cactus and stops 3.5 blocks away.
func TestCamelsFollowCactus(t *testing.T) {
	w := traderTestWorld(t)
	var h *world.EntityHandle
	playerPos := mgl64.Vec3{9.5, 64, .5}
	<-w.Exec(func(tx *world.Tx) {
		h = spawnAnimal(t, tx, "minecraft:camel", .5, nil)
		addSurvivalPlayer(tx, playerPos).SetHeldItems(item.NewStack(block.Cactus{}, 1), item.Stack{})
	})
	closest := 100.0
	for range 300 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) { closest = min(closest, horizontalDistance(entityPos(tx, h), playerPos)) })
	}
	if closest > 4.5 || closest < 2.5 {
		t.Fatalf("camel came within %.2f blocks of a player holding cactus, want it to stop about 3.5 blocks away", closest)
	}
}

// TestHurtCamelsStandUpAndPanic checks CamelAi.CamelPanic: a sitting camel
// that is hurt stands up at once and runs off at four times its speed.
func TestHurtCamelsStandUpAndPanic(t *testing.T) {
	w := traderTestWorld(t)
	// A camel standing up at once still takes 52 ticks of a new world.
	for range 53 {
		w.AdvanceTick()
	}
	var h *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		h = spawnAnimal(t, tx, "minecraft:camel", .5, map[string]any{"Sitting": byte(1), "LastPoseTick": int64(-1)})
	})
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		if !camelSitting(tx, h) {
			t.Fatal("camel loaded sitting does not sit")
		}
	})
	hurt(w, h, playerAttack)
	<-w.Exec(func(tx *world.Tx) {
		if camelSitting(tx, h) {
			t.Fatal("hurt camel kept sitting")
		}
	})
	if !startsPanicking(w, h) {
		t.Fatal("hurt camel does not panic")
	}
	var start mgl64.Vec3
	<-w.Exec(func(tx *world.Tx) { start = entityPos(tx, h) })
	for range 40 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if d := horizontalDistance(entityPos(tx, h), start); d < 2 {
			t.Fatalf("panicking camel ran %.2f blocks in two seconds", d)
		}
	})
}

// TestCamelsIgnoreTheTimeOfDay checks Camel.getPoseTime counts game ticks:
// setting the time of day back does not freeze a camel in a pose change.
func TestCamelsIgnoreTheTimeOfDay(t *testing.T) {
	w := traderTestWorld(t)
	for range 53 {
		w.AdvanceTick()
	}
	w.SetTime(1000000)
	var h *world.EntityHandle
	playerPos := mgl64.Vec3{9.5, 64, .5}
	<-w.Exec(func(tx *world.Tx) { h = spawnAnimal(t, tx, "minecraft:camel", .5, nil) })
	w.AdvanceTick()
	w.SetTime(0)
	<-w.Exec(func(tx *world.Tx) {
		addSurvivalPlayer(tx, playerPos).SetHeldItems(item.NewStack(block.Cactus{}, 1), item.Stack{})
	})
	closest := 100.0
	for range 300 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) { closest = min(closest, horizontalDistance(entityPos(tx, h), playerPos)) })
	}
	if closest > 4.5 {
		t.Fatalf("camel came no closer than %.2f blocks to a player holding cactus after the time of day was set back", closest)
	}
}
