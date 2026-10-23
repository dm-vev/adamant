package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestSquidsSwimAround checks SquidRandomMovementGoal and the tentacle
// strokes of Squid.aiStep: a squid swims about the pool it is in.
func TestSquidsSwimAround(t *testing.T) {
	w := poolTestWorld(t)
	var squid *world.EntityHandle
	start := mgl64.Vec3{.5, 61, .5}
	<-w.Exec(func(tx *world.Tx) {
		squid = tx.AddEntity(entity.NewSquid(world.EntitySpawnOpts{Position: start})).H()
		// Mobs idle without a player within 32 blocks.
		addSurvivalPlayer(tx, mgl64.Vec3{-15.5, 64, .5})
	})
	furthest := 0.0
	for range 600 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			pos := entityPos(tx, squid)
			// A stroke may carry a squid a little out of the water, as it
			// does in Java.
			if pos[1] > 64.5 || pos[1] < 59.9 {
				t.Fatalf("squid left the pool to %v", pos)
			}
			furthest = max(furthest, pos.Sub(start).Len())
		})
	}
	if furthest < 3 {
		t.Fatalf("squid swam no further than %.2f blocks in half a minute", furthest)
	}
}

// TestSquidsFleeWhatHurtThem checks SquidFleeGoal: a squid swims away from
// a player that hurt it.
func TestSquidsFleeWhatHurtThem(t *testing.T) {
	w := poolTestWorld(t)
	var squid *world.EntityHandle
	player := mgl64.Vec3{.5, 61, .5}
	<-w.Exec(func(tx *world.Tx) {
		p := addSurvivalPlayer(tx, player)
		squid = tx.AddEntity(entity.NewSquid(world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 61, .5}})).H()
		e, _ := squid.Entity(tx)
		e.(entity.Living).Hurt(1, entity.AttackDamageSource{Attacker: p})
	})
	for range 100 {
		w.AdvanceTick()
		far := false
		<-w.Exec(func(tx *world.Tx) { far = horizontalDistance(entityPos(tx, squid), player) > 6 })
		if far {
			return
		}
	}
	t.Fatal("hurt squid did not flee its attacker in five seconds")
}

// TestSquidsDrownOnLand checks AgeableWaterCreature.handleAirSupply and the
// fall of Squid.aiStep: a squid on land falls and starts to drown after 16
// seconds.
func TestSquidsDrownOnLand(t *testing.T) {
	w := poolTestWorld(t)
	var squid *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		squid = tx.AddEntity(entity.NewSquid(world.EntitySpawnOpts{Position: mgl64.Vec3{15.5, 66, 15.5}})).H()
	})
	for i := range 340 {
		w.AdvanceTick()
		health, y := 0.0, 0.0
		<-w.Exec(func(tx *world.Tx) {
			e, _ := squid.Entity(tx)
			health, y = e.(entity.Living).Health(), e.Position()[1]
		})
		if i == 40 && y > 64.01 {
			t.Fatalf("squid on land stayed at y %.2f, want it fallen to the ground at 64", y)
		}
		if i < 315 && health < 10 {
			t.Fatalf("squid on land took damage after %d ticks, want it to hold its breath for 16 seconds", i)
		}
		if health < 10 {
			return
		}
	}
	t.Fatal("squid on land did not drown in 17 seconds")
}
