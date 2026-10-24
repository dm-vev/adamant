package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func witherOf(tx *world.Tx, h *world.EntityHandle) *entity.Wither {
	e, ok := h.Entity(tx)
	if !ok {
		return nil
	}
	return e.(*entity.Wither)
}

func findWither(tx *world.Tx) *entity.Wither {
	for e := range tx.Entities() {
		if w, ok := e.(*entity.Wither); ok {
			return w
		}
	}
	return nil
}

func countType(tx *world.Tx, t world.EntityType) int {
	n := 0
	for e := range tx.Entities() {
		if e.H().Type() == t {
			n++
		}
	}
	return n
}

// TestWithersAreSummoned checks WitherSkullBlock.checkSpawn and
// WitherBoss.makeInvulnerable: soul sand in a T with three wither skeleton
// skulls on top summons a wither with a third of its health, which nothing
// hurts while it heals up, and which blows up when it is done.
func TestWithersAreSummoned(t *testing.T) {
	w := wolfTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		for _, pos := range []cube.Pos{{0, 64, 0}, {0, 65, 0}, {-1, 65, 0}, {1, 65, 0}} {
			tx.SetBlock(pos, block.SoulSand{}, nil)
		}
		for x := -1; x <= 1; x++ {
			tx.SetBlock(cube.Pos{x, 66, 0}, block.Skull{Type: block.WitherSkeletonSkull()}, nil)
		}
		if !tx.World().EntityRegistry().Config().WitherPattern(tx, cube.Pos{0, 66, 0}) {
			t.Fatal("soul sand and three wither skeleton skulls did not summon a wither")
		}
		wi := findWither(tx)
		if wi == nil || wi.Health() != 100 || wi.InvulnerableTicks() != 220 {
			t.Fatalf("summoned wither %v, want one with 100 health summoned for 220 ticks", wi)
		}
		if _, hurt := wi.Hurt(10, entity.AttackDamageSource{Attacker: addSurvivalPlayer(tx, mgl64.Vec3{5.5, 64, .5})}); hurt {
			t.Fatal("wither took damage while it was summoned")
		}
	})
	stepWorld(t, w, 225)
	<-w.Exec(func(tx *world.Tx) {
		wi := findWither(tx)
		if wi.InvulnerableTicks() != 0 || wi.Health() < 299 {
			t.Fatalf("wither has %d summoning ticks and %.1f health after 225 ticks, want 0 and 300", wi.InvulnerableTicks(), wi.Health())
		}
	})
}

// TestWithersShootSkullsAtPlayers checks the RangedAttackGoal of the wither
// and the heads of WitherBoss.customServerAiStep: a wither shoots skulls at
// a player nearby, and spares the undead.
func TestWithersShootSkullsAtPlayers(t *testing.T) {
	w := wolfTestWorld(t)
	var wither, zombie *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		wither = tx.AddEntity(entity.NewWither(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 64, .5}})).H()
		zombie = addMob(t, tx, "minecraft:zombie", mgl64.Vec3{-6.5, 64, .5}).H()
		addSurvivalPlayer(tx, mgl64.Vec3{10.5, 64, .5})
	})
	shot := false
	for range 200 {
		stepWorld(t, w, 1)
		<-w.Exec(func(tx *world.Tx) {
			for _, h := range witherOf(tx, wither).HeadTargets() {
				if h == zombie {
					t.Fatal("wither head targets a zombie")
				}
			}
			shot = shot || countType(tx, entity.WitherSkullType) > 0
		})
	}
	if !shot {
		t.Fatal("wither did not shoot a skull at a player ten blocks away in ten seconds")
	}
}

// TestPoweredWithersShrugOffArrows checks WitherBoss.hurtServer: below half
// health the wither's armour turns arrows aside.
func TestPoweredWithersShrugOffArrows(t *testing.T) {
	w := wolfTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		wi := witherOf(tx, tx.AddEntity(entity.NewWither(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 64, .5}})).H())
		p := addSurvivalPlayer(tx, mgl64.Vec3{10.5, 64, .5})
		arrow := tx.AddEntity(tx.World().EntityRegistry().Config().Arrow(world.EntitySpawnOpts{Position: mgl64.Vec3{5.5, 65, .5}}, world.ArrowSpawnConfig{Damage: 2, Owner: p, Tip: potion.Potion{}}))
		src := entity.ProjectileDamageSource{Projectile: arrow, Owner: p}
		if _, hurt := wi.Hurt(4, src); !hurt {
			t.Fatal("arrow did not hurt a wither at full health")
		}
		wi.Hurt(200, entity.AttackDamageSource{Attacker: p})
		if !wi.Powered() {
			t.Fatalf("wither at %.1f health is not powered", wi.Health())
		}
		before := wi.Health()
		if _, hurt := wi.Hurt(4, src); hurt || wi.Health() != before {
			t.Fatal("arrow hurt a powered wither")
		}
	})
}

// TestHurtWithersBreakBlocks checks WitherBoss.customServerAiStep: a second
// after a wither is hurt it breaks the blocks around it.
func TestHurtWithersBreakBlocks(t *testing.T) {
	w := wolfTestWorld(t)
	var wither *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		for x := -1; x <= 1; x++ {
			for y := 64; y <= 67; y++ {
				for z := -1; z <= 1; z++ {
					if x != 0 || z != 0 {
						tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
					}
				}
			}
		}
		// A roof keeps the wither from rising out of its shaft.
		tx.SetBlock(cube.Pos{0, 68, 0}, block.Stone{}, nil)
		wither = tx.AddEntity(entity.NewWither(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 64, .5}})).H()
		witherOf(tx, wither).Hurt(5, entity.AttackDamageSource{Attacker: addSurvivalPlayer(tx, mgl64.Vec3{8.5, 64, .5})})
	})
	stepWorld(t, w, 25)
	<-w.Exec(func(tx *world.Tx) {
		if _, stone := tx.Block(cube.Pos{1, 65, 0}).(block.Stone); stone {
			t.Fatal("hurt wither did not break the stone around it")
		}
	})
}

// TestWitherSkullsSpeedUp checks AbstractHurtingProjectile.applyInertia: a
// wither skull starts slow and speeds up to nearly two blocks a tick.
func TestWitherSkullsSpeedUp(t *testing.T) {
	w := wolfTestWorld(t)
	var skull *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		skull = tx.AddEntity(entity.NewWitherSkull(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 100, .5}, Velocity: mgl64.Vec3{.1, 0, 0}}, nil, false)).H()
	})
	stepWorld(t, w, 40)
	<-w.Exec(func(tx *world.Tx) {
		e, ok := skull.Entity(tx)
		if !ok {
			t.Fatal("skull flying through the air broke")
		}
		if v := e.(interface{ Velocity() mgl64.Vec3 }).Velocity().Len(); v < 1.5 || v > 1.91 {
			t.Fatalf("wither skull flies at %.3f blocks a tick after two seconds, want close to 1.9", v)
		}
	})
}
