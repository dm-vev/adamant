package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestPolarBearsGuardTheirCubs checks PolarBearAttackPlayersGoal and
// PolarBearMeleeAttackGoal: an adult polar bear with a cub near attacks a
// player close by and rears up first, and one without a cub leaves the
// player alone.
func TestPolarBearsGuardTheirCubs(t *testing.T) {
	for _, cub := range []bool{false, true} {
		w := aiTestWorld(t)
		p := addAITestPlayer(w, mgl64.Vec3{4.5, 65, .5}, world.GameModeSurvival)
		var bear *world.EntityHandle
		<-w.Exec(func(tx *world.Tx) {
			bear = tx.AddEntity(entity.NewPolarBear(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}})).H()
			if cub {
				tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{-2.5, 65, .5}}.New(entity.PolarBearType, entity.PolarBearConfig{Age: -24000}))
			}
		})
		stood := false
		lowest := lowestHealth(t, w, p, 300, func(tx *world.Tx) {
			e, _ := bear.Entity(tx)
			stood = stood || e.(*entity.PolarBear).Standing()
		})
		if attacked := lowest < 20; attacked != cub {
			t.Fatalf("polar bear with a cub near %v attacked the player %v, lowest health %v", cub, attacked, lowest)
		}
		if cub && !stood {
			t.Fatal("polar bear attacked without rearing up")
		}
	}
}

// TestHurtPolarBearCubsCallAdults checks PolarBearHurtByTargetGoal: an adult
// polar bear attacks the player that hurt a cub nearby.
func TestHurtPolarBearCubsCallAdults(t *testing.T) {
	w := aiTestWorld(t)
	// The player stands farther than half the follow range, beyond which
	// PolarBearAttackPlayersGoal does not look.
	p := addAITestPlayer(w, mgl64.Vec3{12.5, 65, .5}, world.GameModeSurvival)
	var cub *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		tx.AddEntity(entity.NewPolarBear(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}}))
		cub = tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 65, .5}}.New(entity.PolarBearType, entity.PolarBearConfig{Age: -24000})).H()
	})
	stepWorld(t, w, 1)
	<-w.Exec(func(tx *world.Tx) {
		c, _ := cub.Entity(tx)
		pl, _ := p.Entity(tx)
		c.(entity.Living).Hurt(1, entity.AttackDamageSource{Attacker: pl.(*player.Player)})
	})
	if lowest := lowestHealth(t, w, p, 400, nil); lowest >= 20 {
		t.Fatal("adult polar bear did not attack the player that hurt its cub")
	}
}

// TestPolarBearsHuntFoxes checks the NearestAttackableTargetGoal of adult
// polar bears for foxes.
func TestPolarBearsHuntFoxes(t *testing.T) {
	w := wolfTestWorld(t)
	var fox *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		pen(tx, 6)
		tx.AddEntity(entity.NewPolarBear(world.EntitySpawnOpts{Position: mgl64.Vec3{-3.5, 64, .5}}))
		fox = spawnAnimal(t, tx, "minecraft:fox", 3.5, nil)
	})
	hurt := false
	for range 600 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) { hurt = livingHealth(tx, fox) < 10 })
		if hurt {
			break
		}
	}
	if !hurt {
		t.Fatal("polar bear did not attack a fox in 30 seconds")
	}
}
