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
