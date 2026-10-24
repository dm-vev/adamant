package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func countWindCharges(tx *world.Tx) int { return countEntities(tx, "minecraft:breeze_wind_charge") }

// TestBreezesShootWindCharges checks Shoot and Slide: a breeze fights a
// player in survival with wind charges.
func TestBreezesShootWindCharges(t *testing.T) {
	w := happyGhastWorld(t)
	var b *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		b = spawnAnimal(t, tx, "minecraft:breeze", .5, nil)
		addSurvivalPlayer(tx, mgl64.Vec3{8.5, 64, .5})
	})
	shooting := false
	for range 600 {
		w.AdvanceTick()
		fired := false
		<-w.Exec(func(tx *world.Tx) {
			e, _ := b.Entity(tx)
			shooting = shooting || e.(*entity.Breeze).Shooting()
			fired = countWindCharges(tx) > 0
		})
		if fired {
			if !shooting {
				t.Fatal("breeze fired a wind charge without taking its shooting pose")
			}
			return
		}
	}
	t.Fatal("breeze did not shoot at a player eight blocks away in half a minute")
}

// TestBreezesLongJump checks LongJump: a breeze jumps to a spot behind a
// player far enough away.
func TestBreezesLongJump(t *testing.T) {
	w := happyGhastWorld(t)
	var b *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		b = spawnAnimal(t, tx, "minecraft:breeze", .5, nil)
		addSurvivalPlayer(tx, mgl64.Vec3{12.5, 64, .5})
	})
	for range 1200 {
		w.AdvanceTick()
		jumped := false
		<-w.Exec(func(tx *world.Tx) {
			e, _ := b.Entity(tx)
			jumped = e.(*entity.Breeze).JumpGoalJumping()
		})
		if jumped {
			return
		}
	}
	t.Fatal("breeze did not long jump towards a player twelve blocks away in a minute")
}

// TestBreezesIgnoreCreativePlayers checks BreezeAttackEntitySensor: a breeze
// leaves players in creative mode alone.
func TestBreezesIgnoreCreativePlayers(t *testing.T) {
	w := happyGhastWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		spawnAnimal(t, tx, "minecraft:breeze", .5, nil)
		tx.AddEntity((world.EntitySpawnOpts{}).New(player.Type, player.Config{Position: mgl64.Vec3{6.5, 64, .5}, GameMode: world.GameModeCreative}))
	})
	for range 400 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			if countWindCharges(tx) > 0 {
				t.Fatal("breeze shot at a player in creative mode")
			}
		})
	}
}
