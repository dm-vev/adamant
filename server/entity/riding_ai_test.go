package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// ridingOf returns the vehicle an entity rides, or nil.
func ridingOf(tx *world.Tx, h *world.EntityHandle) *world.EntityHandle {
	e, ok := h.Entity(tx)
	if !ok {
		return nil
	}
	return e.(entity.Rider).Riding()
}

// TestBabyPiglinsRideBabyHoglins checks PiglinAi's ride activity: a baby
// piglin now and then gets on a baby hoglin it sees, sits on top of it, and
// gets off once it is hurt.
func TestBabyPiglinsRideBabyHoglins(t *testing.T) {
	w := happyGhastWorld(t)
	var piglin, hoglin *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		// A pen keeps them in sight of each other.
		for x := -4; x <= 7; x++ {
			for z := -5; z <= 6; z++ {
				if x == -4 || x == 7 || z == -5 || z == 6 {
					tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
					tx.SetBlock(cube.Pos{x, 65, z}, block.Stone{}, nil)
				}
			}
		}
		piglin = spawnAnimal(t, tx, "minecraft:piglin", .5, map[string]any{"IsBaby": byte(1), "IsImmuneToZombification": byte(1)})
		hoglin = spawnAnimal(t, tx, "minecraft:hoglin", 3.5, map[string]any{"Age": int32(-24000), "IsImmuneToZombification": byte(1)})
	})
	riding := false
	for range 4000 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) { riding = ridingOf(tx, piglin) == hoglin })
		if riding {
			break
		}
	}
	if !riding {
		t.Fatal("baby piglin did not get on a baby hoglin near in over three minutes")
	}
	for range 5 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		p, _ := piglin.Entity(tx)
		h, _ := hoglin.Entity(tx)
		if d := p.Position().Sub(h.Position()); d[1] < .5 || d[1] > 1 || d[0]*d[0]+d[2]*d[2] > .01 {
			t.Fatalf("baby piglin rides at %v from the baby hoglin, want on top of it", d)
		}
		p.(entity.Living).Hurt(1, entity.GenericDamageSource{})
	})
	for range 40 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) { riding = ridingOf(tx, piglin) != nil })
		if !riding {
			return
		}
	}
	t.Fatal("hurt baby piglin stayed on its baby hoglin")
}

// TestChickenJockeysChasePlayers checks Mob.getNavigation and
// updateControlFlags: a baby zombie riding a chicken walks it up to a
// player with its own goals and attacks.
func TestChickenJockeysChasePlayers(t *testing.T) {
	w := happyGhastWorld(t)
	var chicken, p *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		chicken = spawnAnimal(t, tx, "minecraft:chicken", .5, nil)
		zombie := spawnAnimal(t, tx, "minecraft:zombie", .5, map[string]any{"IsBaby": byte(1)})
		c, _ := chicken.Entity(tx)
		z, _ := zombie.Entity(tx)
		if !entity.StartRiding(tx, z, c) {
			t.Fatal("baby zombie did not get on a chicken")
		}
		p = addSurvivalPlayer(tx, mgl64.Vec3{10.5, 64, .5}).H()
	})
	for range 600 {
		w.AdvanceTick()
		hurt := false
		<-w.Exec(func(tx *world.Tx) { hurt = playerHurt(tx, p) })
		if hurt {
			<-w.Exec(func(tx *world.Tx) {
				c, _ := chicken.Entity(tx)
				if c.Position()[0] < 7 {
					t.Fatalf("chicken jockey hurt a player ten blocks away from %v", c.Position())
				}
			})
			return
		}
	}
	t.Fatal("chicken jockey did not chase down a player ten blocks away in half a minute")
}

// TestRavagerRidersStayOnTop checks Entity.positionRider for a vehicle that
// keeps no seats: a pillager riding a ravager stays on its back as it
// moves.
func TestRavagerRidersStayOnTop(t *testing.T) {
	w := happyGhastWorld(t)
	var ravager, pillager *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		ravager = spawnAnimal(t, tx, "minecraft:ravager", .5, nil)
		pillager = spawnAnimal(t, tx, "minecraft:pillager", .5, nil)
		r, _ := ravager.Entity(tx)
		p, _ := pillager.Entity(tx)
		if !entity.StartRiding(tx, p, r) {
			t.Fatal("pillager did not get on a ravager")
		}
		r.(interface{ SetVelocity(mgl64.Vec3) }).SetVelocity(mgl64.Vec3{.5, 0, .3})
	})
	for range 40 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		r, _ := ravager.Entity(tx)
		p, _ := pillager.Entity(tx)
		if ridingOf(tx, pillager) != ravager {
			t.Fatal("pillager no longer rides the ravager")
		}
		if pos := r.Position(); (pos[0]-.5)*(pos[0]-.5)+(pos[2]-.5)*(pos[2]-.5) < .25 {
			t.Fatalf("ravager pushed along stayed at %v", pos)
		}
		if d := p.Position().Sub(r.Position()); d[1] < 1.5 || d[0]*d[0]+d[2]*d[2] > .1 {
			t.Fatalf("pillager rides %v from the ravager, want on its back", d)
		}
	})
}
