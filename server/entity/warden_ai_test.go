package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// spawnWarden spawns a warden that may not dig away for a minute, as
// Warden.finalizeSpawn leaves it.
func spawnWarden(t *testing.T, tx *world.Tx, pos mgl64.Vec3, reason string) *world.EntityHandle {
	t.Helper()
	return tx.AddEntity(entity.NewWarden(world.EntitySpawnOpts{Position: pos, SpawnReason: reason})).H()
}

func warden(tx *world.Tx, h *world.EntityHandle) *entity.Warden {
	e, _ := h.Entity(tx)
	return e.(*entity.Warden)
}

// TestWardensHearVibrations checks Warden.VibrationUser: a warden hears the
// steps of a player and grows angry at them.
func TestWardensHearVibrations(t *testing.T) {
	w := happyGhastWorld(t)
	var wd, p *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		wd = spawnWarden(t, tx, mgl64.Vec3{.5, 64, .5}, "spawn_egg")
		p = addSurvivalPlayer(tx, mgl64.Vec3{8.5, 64, .5}).H()
	})
	for range 5 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		pl, _ := p.Entity(tx)
		tx.EmitGameEvent(world.GameEventStep, pl.Position(), world.GameEventContext{Source: pl})
	})
	for range 40 {
		w.AdvanceTick()
		angry := false
		<-w.Exec(func(tx *world.Tx) {
			pl, _ := p.Entity(tx)
			angry = warden(tx, wd).AngerAt(pl) > 0
		})
		if angry {
			return
		}
	}
	t.Fatal("warden did not grow angry at a player stepping eight blocks away")
}

// TestWardensFightAttackers checks Warden.hurtServer, Roar-free targeting
// and MeleeAttack: a warden hit by a player close by fights them.
func TestWardensFightAttackers(t *testing.T) {
	w := happyGhastWorld(t)
	var wd, p *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		wd = spawnWarden(t, tx, mgl64.Vec3{.5, 64, .5}, "spawn_egg")
		p = addSurvivalPlayer(tx, mgl64.Vec3{3.5, 64, .5}).H()
	})
	for range 5 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		pl, _ := p.Entity(tx)
		warden(tx, wd).Hurt(1, entity.AttackDamageSource{Attacker: pl})
	})
	for range 400 {
		w.AdvanceTick()
		hurt := false
		<-w.Exec(func(tx *world.Tx) { hurt = playerHurt(tx, p) })
		if hurt {
			return
		}
	}
	t.Fatal("warden did not fight a player that hit it from three blocks away")
}

// TestWardensSonicBoomUnreachableTargets checks SonicBoom: a warden fighting
// a target it cannot reach blasts it.
func TestWardensSonicBoomUnreachableTargets(t *testing.T) {
	w := happyGhastWorld(t)
	var wd, p *world.EntityHandle
	target := mgl64.Vec3{10.5, 70, .5}
	<-w.Exec(func(tx *world.Tx) {
		wd = spawnWarden(t, tx, mgl64.Vec3{.5, 64, .5}, "spawn_egg")
		p = tx.AddEntity((world.EntitySpawnOpts{}).New(player.Type, player.Config{Position: target, GameMode: world.GameModeSurvival})).H()
	})
	for range 5 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		pl, _ := p.Entity(tx)
		warden(tx, wd).Hurt(1, entity.AttackDamageSource{Attacker: pl})
	})
	boomed := false
	for range 600 {
		w.AdvanceTick()
		hurt := false
		<-w.Exec(func(tx *world.Tx) {
			pl, _ := p.Entity(tx)
			// The player floats out of reach.
			pl.(*player.Player).Teleport(target)
			_, _, _, _, sonic := warden(tx, wd).WardenPose()
			boomed = boomed || sonic
			hurt = playerHurt(tx, p)
		})
		if hurt {
			if !boomed {
				t.Fatal("warden hurt a player out of reach without its sonic boom")
			}
			return
		}
	}
	t.Fatal("warden did not sonic boom a player out of its reach in half a minute")
}

// TestWardensDigAway checks Digging: a warden off its dig cooldown with
// nothing to do digs into the ground and is gone.
func TestWardensDigAway(t *testing.T) {
	w := happyGhastWorld(t)
	var wd *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) { wd = spawnAnimal(t, tx, "minecraft:warden", .5, nil) })
	digging := false
	for range 400 {
		w.AdvanceTick()
		gone := false
		<-w.Exec(func(tx *world.Tx) {
			e, ok := wd.Entity(tx)
			if !ok {
				gone = true
				return
			}
			_, dig, _, _, _ := e.(*entity.Warden).WardenPose()
			digging = digging || dig
		})
		if gone {
			if !digging {
				t.Fatal("warden vanished without digging")
			}
			return
		}
	}
	t.Fatal("warden off its dig cooldown did not dig away in twenty seconds")
}

// TestWardensDarkenPlayers checks Warden.applyDarknessAround.
func TestWardensDarkenPlayers(t *testing.T) {
	w := happyGhastWorld(t)
	var p *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		spawnWarden(t, tx, mgl64.Vec3{.5, 64, .5}, "spawn_egg")
		p = addSurvivalPlayer(tx, mgl64.Vec3{12.5, 64, .5}).H()
	})
	for range 130 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		pl, _ := p.Entity(tx)
		if _, ok := pl.(*player.Player).Effect(effect.Darkness); !ok {
			t.Fatal("player twelve blocks from a warden was not darkened in six seconds")
		}
	})
}

// TestWardensEmerge checks Warden.finalizeSpawn and Emerging: a summoned
// warden emerges from the ground for almost seven seconds, invulnerable.
func TestWardensEmerge(t *testing.T) {
	w := happyGhastWorld(t)
	var wd *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) { wd = spawnWarden(t, tx, mgl64.Vec3{.5, 64, .5}, "triggered") })
	for range 100 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		wa := warden(tx, wd)
		if emerging, _, _, _, _ := wa.WardenPose(); !emerging {
			t.Fatal("summoned warden is not emerging five seconds after it was summoned")
		}
		if _, hurt := wa.Hurt(10, entity.GenericDamageSource{}); hurt {
			t.Fatal("emerging warden took damage")
		}
	})
	for range 60 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if emerging, _, _, _, _ := warden(tx, wd).WardenPose(); emerging {
			t.Fatal("warden still emerging eight seconds after it was summoned")
		}
	})
}
