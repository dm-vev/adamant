package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestIronGolemsFightEnemies checks the NearestAttackableTargetGoal of iron
// golems for enemies: a golem crushes a zombie nearby and flings it up.
func TestIronGolemsFightEnemies(t *testing.T) {
	w := aiTestWorld(t)
	var zombie *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		tx.AddEntity(entity.NewIronGolem(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}}))
		zombie = tx.AddEntity(entity.NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{4.5, 65, .5}})).H()
	})
	hit := false
	for range 400 {
		stepWorld(t, w, 1)
		<-w.Exec(func(tx *world.Tx) { hit = livingHealth(tx, zombie) <= 20-7.5 })
		if hit {
			break
		}
	}
	if !hit {
		t.Fatal("iron golem did not hit a zombie four blocks away for at least 7.5")
	}
}

// TestIronGolemsTurnOnWhoeverHurtsThem checks HurtByTargetGoal and
// IronGolem.canAttack: a village golem hurt by a player fights it, but one
// a player built never attacks players.
func TestIronGolemsTurnOnWhoeverHurtsThem(t *testing.T) {
	for _, built := range []bool{false, true} {
		w := aiTestWorld(t)
		p := addAITestPlayer(w, mgl64.Vec3{3.5, 65, .5}, world.GameModeSurvival)
		var golem *world.EntityHandle
		<-w.Exec(func(tx *world.Tx) {
			golem = tx.AddEntity(entity.NewIronGolem(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}})).H()
			e, _ := golem.Entity(tx)
			e.(*entity.IronGolem).SetPlayerCreated(built)
		})
		stepWorld(t, w, 1)
		<-w.Exec(func(tx *world.Tx) {
			e, _ := golem.Entity(tx)
			pl, _ := p.Entity(tx)
			e.(entity.Living).Hurt(1, entity.AttackDamageSource{Attacker: pl.(*player.Player)})
		})
		lowest := lowestHealth(t, w, p, 200, nil)
		if attacked := lowest < 20; attacked == built {
			t.Fatalf("golem built by a player %v attacked the player that hurt it %v, lowest health %v", built, attacked, lowest)
		}
	}
}

// TestSnowGolemsPeltEnemies checks the RangedAttackGoal of snow golems: a
// snow golem throws snowballs at a zombie nearby.
func TestSnowGolemsPeltEnemies(t *testing.T) {
	w := aiTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		tx.AddEntity(entity.NewSnowGolem(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}}))
		tx.AddEntity(entity.NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{6.5, 65, .5}}))
	})
	thrown := false
	for range 200 {
		stepWorld(t, w, 1)
		<-w.Exec(func(tx *world.Tx) { thrown = thrown || countEntities(tx, "minecraft:snowball") > 0 })
		if thrown {
			break
		}
	}
	if !thrown {
		t.Fatal("snow golem threw no snowball at a zombie six blocks away")
	}
}

// TestSnowGolemsLeaveSnow checks SnowGolem.aiStep: a snow golem leaves snow
// where it stands.
func TestSnowGolemsLeaveSnow(t *testing.T) {
	w := aiTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		tx.AddEntity(entity.NewSnowGolem(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}}))
	})
	stepWorld(t, w, 5)
	<-w.Exec(func(tx *world.Tx) {
		snow := 0
		for x := -16; x <= 16; x++ {
			for z := -16; z <= 16; z++ {
				if _, ok := tx.Block(cube.Pos{x, 65, z}).(block.SnowLayer); ok {
					snow++
				}
			}
		}
		if snow == 0 {
			t.Fatal("snow golem left no snow")
		}
	})
}
