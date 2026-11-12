package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func addCat(tx *world.Tx, pos mgl64.Vec3, conf entity.CatBehaviourConfig) *world.EntityHandle {
	return tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(entity.CatType, conf)).H()
}

// TestStrayCatsFleeFromPlayers checks CatAvoidEntityGoal: a stray cat runs
// from a player that comes near.
func TestStrayCatsFleeFromPlayers(t *testing.T) {
	t.Parallel()
	w := wolfTestWorld(t)
	var cat *world.EntityHandle
	playerPos := mgl64.Vec3{4.5, 64, .5}
	<-w.Exec(func(tx *world.Tx) {
		cat = addCat(tx, mgl64.Vec3{.5, 64, .5}, entity.CatBehaviourConfig{})
		addSurvivalPlayer(tx, playerPos)
	})
	farthest := 0.0
	for range 100 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) { farthest = max(farthest, horizontalDistance(entityPos(tx, cat), playerPos)) })
	}
	if farthest < 8 {
		t.Fatalf("stray cat got at most %.2f blocks from a player four blocks away, want it to flee", farthest)
	}
}

// TestStrayCatsCreepToFish checks CatTemptGoal: a stray cat creeps up to a
// player holding raw cod.
func TestStrayCatsCreepToFish(t *testing.T) {
	t.Parallel()
	w := wolfTestWorld(t)
	var cat *world.EntityHandle
	playerPos := mgl64.Vec3{8.5, 64, .5}
	<-w.Exec(func(tx *world.Tx) {
		cat = addCat(tx, mgl64.Vec3{.5, 64, .5}, entity.CatBehaviourConfig{})
		addSurvivalPlayer(tx, playerPos).SetHeldItems(item.NewStack(item.Cod{}, 1), item.Stack{})
	})
	closest, crouched := 100.0, false
	for range 300 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			closest = min(closest, horizontalDistance(entityPos(tx, cat), playerPos))
			e, _ := cat.Entity(tx)
			crouched = crouched || e.(*entity.Cat).Crouching()
		})
	}
	if closest > 3.5 {
		t.Fatalf("stray cat came no closer than %.2f blocks to a player holding cod", closest)
	}
	if !crouched {
		t.Fatal("tempted cat never crept")
	}
}

// TestTamedCatsFollowTheirOwner checks FollowOwnerGoal for cats, which come
// back to within five blocks of their owner.
func TestTamedCatsFollowTheirOwner(t *testing.T) {
	t.Parallel()
	w := wolfTestWorld(t)
	var cat *world.EntityHandle
	ownerPos := mgl64.Vec3{12.5, 64, .5}
	<-w.Exec(func(tx *world.Tx) {
		p := addSurvivalPlayer(tx, ownerPos)
		cat = addCat(tx, mgl64.Vec3{.5, 64, .5}, entity.CatBehaviourConfig{Tamed: true, OwnerUUID: p.UUID().String()})
	})
	closest := 100.0
	for range 200 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) { closest = min(closest, horizontalDistance(entityPos(tx, cat), ownerPos)) })
	}
	if closest > 6 {
		t.Fatalf("tamed cat came no closer than %.2f blocks to its owner", closest)
	}
}

// huntRabbit lets a stray cat hunt a rabbit in a pen for up to 30 seconds. It
// reports whether the rabbit was hurt and whether the hunt stalled instead:
// the cat and the rabbit stood still for the last five seconds within two
// blocks of each other.
//
// OcelotAttackGoal scratches only within 1.2 blocks (twice the width of the
// cat) of the middle of the target, while the cat's path ends in the block
// next to the rabbit's, where the middle of the block may be up to 1.4 blocks
// from a rabbit at the far side of its own. As in Java the cat then waits for
// the rabbit to hop on, which the rabbit does about once in 120 ticks; in
// about one hunt in a thousand it does not hop for the rest of the 30 seconds.
func huntRabbit(t *testing.T) (hurt, stalled bool) {
	t.Helper()
	w := wolfTestWorld(t)
	var rabbit, cat *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		pen(tx, 6)
		cat = addCat(tx, mgl64.Vec3{-3.5, 64, .5}, entity.CatBehaviourConfig{})
		rabbit = spawnAnimal(t, tx, "minecraft:rabbit", 3.5, nil)
	})
	var catPos, rabbitPos mgl64.Vec3
	stillSince := 0
	for tick := range 600 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			hurt = livingHealth(tx, rabbit) < 3
			if hurt {
				// A rabbit that died is gone from the world.
				return
			}
			c, p := entityPos(tx, cat), entityPos(tx, rabbit)
			if c.Sub(catPos).Len() > 1e-3 || p.Sub(rabbitPos).Len() > 1e-3 {
				catPos, rabbitPos, stillSince = c, p, tick
			}
		})
		if hurt {
			return true, false
		}
	}
	return false, catPos.Sub(rabbitPos).Len() < 2 && 600-stillSince >= 100
}

// TestStrayCatsHuntRabbits checks the NonTameRandomTargetGoal of cats and
// OcelotAttackGoal: a stray cat stalks a rabbit and scratches it.
func TestStrayCatsHuntRabbits(t *testing.T) {
	t.Parallel()
	// A hunt that stalled is tried again, up to five times; one that did not
	// and ended with an unhurt rabbit fails at once.
	for attempt := 1; ; attempt++ {
		hurt, stalled := huntRabbit(t)
		if hurt {
			return
		}
		if !stalled {
			t.Fatal("stray cat did not attack a rabbit in 30 seconds")
		}
		if attempt == 5 {
			t.Fatalf("stray cat stood by a rabbit out of reach in %d hunts in a row", attempt)
		}
	}
}

// TestOcelotsHuntChickens checks the NearestAttackableTargetGoal of ocelots
// for chickens.
func TestOcelotsHuntChickens(t *testing.T) {
	t.Parallel()
	w := wolfTestWorld(t)
	var chicken *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		pen(tx, 6)
		tx.AddEntity(entity.NewOcelot(world.EntitySpawnOpts{Position: mgl64.Vec3{-3.5, 64, .5}}))
		chicken = spawnAnimal(t, tx, "minecraft:chicken", 3.5, nil)
	})
	hurt := false
	for range 600 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) { hurt = livingHealth(tx, chicken) < 4 })
		if hurt {
			break
		}
	}
	if !hurt {
		t.Fatal("ocelot did not attack a chicken in 30 seconds")
	}
}

// TestTamedCatsLieOnBeds checks CatLieOnBlockGoal: a tamed cat walks to a bed
// nearby and lies down on it. Searching from two blocks down, Java's
// MoveToBlockGoal looks three blocks below the cat before the blocks at its
// own level, which it skips, so the bed lies in a pit.
func TestTamedCatsLieOnBeds(t *testing.T) {
	t.Parallel()
	w := wolfTestWorld(t)
	var cat *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		for x := 3; x <= 6; x++ {
			for z := -1; z <= 1; z++ {
				for y := 61; y <= 63; y++ {
					tx.SetBlock(cube.Pos{x, y, z}, nil, nil)
				}
				tx.SetBlock(cube.Pos{x, 60, z}, block.Stone{}, nil)
			}
		}
		tx.SetBlock(cube.Pos{4, 61, 0}, block.Bed{Facing: cube.East}, nil)
		tx.SetBlock(cube.Pos{5, 61, 0}, block.Bed{Facing: cube.East, Head: true}, nil)
		p := addSurvivalPlayer(tx, mgl64.Vec3{.5, 64, 3.5})
		cat = addCat(tx, mgl64.Vec3{.5, 64, .5}, entity.CatBehaviourConfig{Tamed: true, OwnerUUID: p.UUID().String()})
	})
	lying := false
	for range 600 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			e, _ := cat.Entity(tx)
			lying = e.(*entity.Cat).Lying()
		})
		if lying {
			break
		}
	}
	if !lying {
		t.Fatal("tamed cat did not lie down on a bed nearby in 30 seconds")
	}
}
