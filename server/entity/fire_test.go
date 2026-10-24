package entity_test

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
)

// TestLavaHurtsMobs checks Entity.lavaHurt reaches mobs: a cow standing in
// lava burns to death in two seconds.
func TestLavaHurtsMobs(t *testing.T) {
	w := wolfTestWorld(t)
	var cow *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 64, 0}, block.Lava{Still: true, Depth: 8}, nil)
		cow = spawnAnimal(t, tx, "minecraft:cow", .5, nil)
	})
	stepWorld(t, w, 40)
	<-w.Exec(func(tx *world.Tx) {
		if e, ok := cow.Entity(tx); ok {
			t.Fatalf("cow in lava for two seconds lives with %.1f health", e.(entity.Living).Health())
		}
	})
}

// TestFireImmuneMobsIgnoreLava checks Entity.fireImmune: lava and fire
// neither hurt nor ignite the mobs Java builds immune to fire.
func TestFireImmuneMobsIgnoreLava(t *testing.T) {
	for _, id := range []string{"minecraft:ghast", "minecraft:blaze", "minecraft:magma_cube", "minecraft:wither_skeleton", "minecraft:strider"} {
		t.Run(id, func(t *testing.T) {
			w := wolfTestWorld(t)
			var h *world.EntityHandle
			<-w.Exec(func(tx *world.Tx) {
				for x := -3; x <= 3; x++ {
					for z := -3; z <= 3; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, block.Lava{Still: true, Depth: 8}, nil)
						tx.SetBlock(cube.Pos{x, 65, z}, block.Lava{Still: true, Depth: 8}, nil)
					}
				}
				tx.SetBlock(cube.Pos{0, 66, 0}, block.Fire{}, nil)
				h = spawnAnimal(t, tx, id, .5, nil)
			})
			for range 40 {
				stepWorld(t, w, 1)
				<-w.Exec(func(tx *world.Tx) {
					e, ok := h.Entity(tx)
					if !ok {
						t.Fatal("mob immune to fire died in lava")
					}
					l := e.(entity.Living)
					if l.Health() < l.MaxHealth() {
						t.Fatalf("lava hurt the mob immune to fire to %.1f health", l.Health())
					}
					if e.(interface{ OnFireDuration() time.Duration }).OnFireDuration() > 0 {
						t.Fatal("mob immune to fire burns")
					}
				})
			}
		})
	}
}

// TestBurningMobsTakeDamage checks the fire of Entity.baseTick: a mob on fire
// takes a point of damage a second until the fire burns out.
func TestBurningMobsTakeDamage(t *testing.T) {
	w := wolfTestWorld(t)
	var cow *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		cow = spawnAnimal(t, tx, "minecraft:cow", .5, nil)
		e, _ := cow.Entity(tx)
		e.(interface{ SetOnFire(time.Duration) }).SetOnFire(5 * time.Second)
	})
	stepWorld(t, w, 120)
	<-w.Exec(func(tx *world.Tx) {
		e, _ := cow.Entity(tx)
		if h := e.(entity.Living).Health(); h != 5 {
			t.Fatalf("cow on fire for five seconds has %.1f health, want 5", h)
		}
		if e.(interface{ OnFireDuration() time.Duration }).OnFireDuration() > 0 {
			t.Fatal("cow still burns after its fire burnt out")
		}
	})
}

// TestRainPutsOutBurningMobs checks Entity.move: rain puts out a mob on fire.
func TestRainPutsOutBurningMobs(t *testing.T) {
	w := wolfTestWorld(t)
	w.StartRaining(time.Hour)
	var cow *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		cow = spawnAnimal(t, tx, "minecraft:cow", .5, nil)
		e, _ := cow.Entity(tx)
		e.(interface{ SetOnFire(time.Duration) }).SetOnFire(5 * time.Second)
	})
	stepWorld(t, w, 2)
	<-w.Exec(func(tx *world.Tx) {
		e, _ := cow.Entity(tx)
		if e.(interface{ OnFireDuration() time.Duration }).OnFireDuration() > 0 {
			t.Fatal("cow in the rain still burns")
		}
	})
}

// TestZombiesBurnInDaylight checks Mob.burnUndead with the fire of
// Entity.baseTick: a zombie under the noon sun burns to death.
func TestZombiesBurnInDaylight(t *testing.T) {
	w := wolfTestWorld(t)
	w.SetTime(6000)
	var zombie *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		zombie = spawnAnimal(t, tx, "minecraft:zombie", .5, nil)
	})
	for range 1200 {
		stepWorld(t, w, 1)
		dead := false
		<-w.Exec(func(tx *world.Tx) { _, alive := zombie.Entity(tx); dead = !alive })
		if dead {
			return
		}
	}
	t.Fatal("zombie under the noon sun did not burn to death in a minute")
}
