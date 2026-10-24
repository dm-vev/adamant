package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func sulfurCubeOf(tx *world.Tx, h *world.EntityHandle) *entity.SulfurCube {
	e, ok := h.Entity(tx)
	if !ok {
		return nil
	}
	return e.(*entity.SulfurCube)
}

// addSulfurCube adds an adult sulfur cube, holding the block passed if any.
func addSulfurCube(tx *world.Tx, pos mgl64.Vec3, body world.Item) *world.EntityHandle {
	conf := entity.SulfurCubeBehaviourConfig{Size: 2}
	if body != nil {
		conf.BodyItem = item.NewStack(body, 1)
	}
	return tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(entity.SulfurCubeType, conf)).H()
}

// TestSulfurCubesHopAbout checks the goals of AbstractCubeMob: a sulfur cube
// hops about in random directions.
func TestSulfurCubesHopAbout(t *testing.T) {
	w := wolfTestWorld(t)
	var cube *world.EntityHandle
	start := mgl64.Vec3{.5, 64, .5}
	<-w.Exec(func(tx *world.Tx) { cube = addSulfurCube(tx, start, nil) })
	jumped, path, last := false, 0.0, start
	for range 400 {
		stepWorld(t, w, 1)
		<-w.Exec(func(tx *world.Tx) {
			c := sulfurCubeOf(tx, cube)
			jumped = jumped || c.Velocity()[1] > .3
			path += horizontalDistance(c.Position(), last)
			last = c.Position()
		})
	}
	if !jumped || path < 6 {
		t.Fatalf("sulfur cube hopped %v and covered %.2f blocks in 20 seconds", jumped, path)
	}
}

// TestSulfurCubesSwallowBlocks checks SulfurCubeSearchForItemsGoal and
// SulfurCube.pickUpItem: a sulfur cube hops to a block lying nearby and
// swallows it, taking on its archetype, and then sits still.
func TestSulfurCubesSwallowBlocks(t *testing.T) {
	w := wolfTestWorld(t)
	var cube *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		cube = addSulfurCube(tx, mgl64.Vec3{.5, 64, .5}, nil)
		tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{4.5, 64, .5}}, item.NewStack(block.Stone{}, 1)))
	})
	swallowed := false
	for range 1200 {
		stepWorld(t, w, 1)
		<-w.Exec(func(tx *world.Tx) { swallowed = !sulfurCubeOf(tx, cube).BodyItem().Empty() })
		if swallowed {
			break
		}
	}
	if !swallowed {
		t.Fatal("sulfur cube did not swallow the stone four blocks away in a minute")
	}
	var at mgl64.Vec3
	<-w.Exec(func(tx *world.Tx) {
		c := sulfurCubeOf(tx, cube)
		if a := c.Archetype(); a != "slow_bouncy" {
			t.Fatalf("sulfur cube holding stone has the %s archetype, want slow_bouncy", a)
		}
		at = c.Position()
	})
	stepWorld(t, w, 100)
	if moved := horizontalDistance(aiEntityPos(w, cube), at); moved > .5 {
		t.Fatalf("sulfur cube holding a block moved %.2f blocks on its own", moved)
	}
}

// TestSulfurCubesTakeOnlyTheirFood checks #sulfur_cube_food and
// #sulfur_cube_swallowable: a baby cube eats slime balls to grow, and an
// adult swallows blocks but not coal.
func TestSulfurCubesTakeOnlyTheirFood(t *testing.T) {
	w := wolfTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		p := addSurvivalPlayer(tx, mgl64.Vec3{3.5, 64, .5})
		adult := sulfurCubeOf(tx, addSulfurCube(tx, mgl64.Vec3{.5, 64, .5}, nil))
		p.SetHeldItems(item.NewStack(item.Coal{}, 1), item.Stack{})
		if adult.Interact(tx, p, &item.UseContext{}) || !adult.BodyItem().Empty() {
			t.Fatal("sulfur cube swallowed coal")
		}
		p.SetHeldItems(item.NewStack(block.Planks{Wood: block.OakWood()}, 1), item.Stack{})
		if !adult.Interact(tx, p, &item.UseContext{}) || adult.Archetype() != "bouncy" {
			t.Fatalf("sulfur cube given planks has the %s archetype, want bouncy", adult.Archetype())
		}
		baby := sulfurCubeOf(tx, tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{-3.5, 64, .5}}.New(entity.SulfurCubeType, entity.SulfurCubeBehaviourConfig{Size: 1, Age: -24000})).H())
		p.SetHeldItems(item.NewStack(item.Slimeball{}, 1), item.Stack{})
		if !baby.Interact(tx, p, &item.UseContext{}) {
			t.Fatal("baby sulfur cube did not eat a slime ball")
		}
	})
}

// TestHitSulfurCubesHoldingBlocksFlyOff checks SulfurCube.hurtServer and
// knockback: a blow does not hurt a cube holding a block but sends it
// flying.
func TestHitSulfurCubesHoldingBlocksFlyOff(t *testing.T) {
	w := wolfTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		p := addSurvivalPlayer(tx, mgl64.Vec3{-1.5, 64, .5})
		// A yaw of -90 looks at the cube along positive x.
		p.Move(mgl64.Vec3{}, -90-p.Rotation().Yaw(), -p.Rotation().Pitch())
		c := sulfurCubeOf(tx, addSulfurCube(tx, mgl64.Vec3{.5, 64, .5}, block.Dirt{}))
		if _, hurt := c.Hurt(4, entity.AttackDamageSource{Attacker: p}); !hurt {
			t.Fatal("blow at a sulfur cube holding dirt did not land")
		}
		if c.Health() < c.MaxHealth() {
			t.Fatalf("blow hurt a sulfur cube holding dirt to %.1f health", c.Health())
		}
		c.KnockBack(p.Position(), .4, .4)
		if v := c.Velocity(); v[0] <= .1 {
			t.Fatalf("hit sulfur cube flies off at %v, want away from the player along positive x", v)
		}
	})
}

// TestBouncySulfurCubesBounce checks Entity.restituteMovementAfterCollisions
// with the bounciness of an archetype: a cube holding planks dropped onto
// stone bounces back up.
func TestBouncySulfurCubesBounce(t *testing.T) {
	w := wolfTestWorld(t)
	var cube *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		cube = addSulfurCube(tx, mgl64.Vec3{.5, 70, .5}, block.Planks{Wood: block.OakWood()})
	})
	fell, bounced := false, false
	for range 100 {
		stepWorld(t, w, 1)
		<-w.Exec(func(tx *world.Tx) {
			v := sulfurCubeOf(tx, cube).Velocity()[1]
			fell = fell || v < -.5
			bounced = bounced || fell && v > .3
		})
	}
	if !bounced {
		t.Fatal("bouncy sulfur cube dropped onto stone did not bounce")
	}
}

// TestSulfurCubesHoldingTNTExplode checks SulfurCube.primeTime and tickFuse:
// flint and steel lights a cube holding TNT, which blows up six seconds
// later.
func TestSulfurCubesHoldingTNTExplode(t *testing.T) {
	w := wolfTestWorld(t)
	var cube *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		p := addSurvivalPlayer(tx, mgl64.Vec3{8.5, 64, .5})
		cube = addSulfurCube(tx, mgl64.Vec3{.5, 64, .5}, block.TNT{})
		p.SetHeldItems(item.NewStack(item.FlintAndSteel{}, 1), item.Stack{})
		c := sulfurCubeOf(tx, cube)
		if !c.Interact(tx, p, &item.UseContext{}) || !c.Primed() {
			t.Fatal("flint and steel did not prime the sulfur cube holding TNT")
		}
	})
	stepWorld(t, w, 110)
	<-w.Exec(func(tx *world.Tx) {
		if sulfurCubeOf(tx, cube) == nil {
			t.Fatal("primed sulfur cube blew up before its six seconds were up")
		}
	})
	stepWorld(t, w, 20)
	<-w.Exec(func(tx *world.Tx) {
		if sulfurCubeOf(tx, cube) != nil {
			t.Fatal("primed sulfur cube did not blow up after six seconds")
		}
	})
}
