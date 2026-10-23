package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// wolfTestWorld returns a stone floor at y 63 around the origin.
func wolfTestWorld(t *testing.T) *world.World {
	t.Helper()
	w := traderTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		for x := -20; x <= 20; x++ {
			for z := -20; z <= 20; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
	})
	return w
}

func addWolf(tx *world.Tx, pos mgl64.Vec3, conf entity.WolfBehaviourConfig) *world.EntityHandle {
	return tx.AddEntity(entity.NewWolf(world.EntitySpawnOpts{Position: pos}, conf)).H()
}

func livingHealth(tx *world.Tx, h *world.EntityHandle) float64 {
	e, ok := h.Entity(tx)
	if !ok {
		return 0
	}
	return e.(entity.Living).Health()
}

// TestWildWolvesHuntSheep checks NonTameRandomTargetGoal with
// Wolf.PREY_SELECTOR: a wild wolf hunts down a sheep nearby.
func TestWildWolvesHuntSheep(t *testing.T) {
	w := wolfTestWorld(t)
	var sheep *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		addWolf(tx, mgl64.Vec3{.5, 64, .5}, entity.WolfBehaviourConfig{})
		sheep = spawnAnimal(t, tx, "minecraft:sheep", 6.5, nil)
	})
	hurt := false
	for range 600 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) { hurt = livingHealth(tx, sheep) < 8 })
		if hurt {
			break
		}
	}
	if !hurt {
		t.Fatal("wild wolf did not attack a sheep six blocks away in 30 seconds")
	}
}

// TestWolvesAttackSkeletons checks the NearestAttackableTargetGoal of wolves
// for AbstractSkeleton.
func TestWolvesAttackSkeletons(t *testing.T) {
	w := wolfTestWorld(t)
	w.SetTime(18000)
	var skeleton *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		// A skeleton flees from wolves as fast as they run, so it is penned
		// in.
		pen(tx, 4)
		addWolf(tx, mgl64.Vec3{-2.5, 64, .5}, entity.WolfBehaviourConfig{})
		skeleton = tx.AddEntity(entity.NewSkeleton(world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 64, .5}})).H()
	})
	hurt := false
	for range 400 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) { hurt = livingHealth(tx, skeleton) < 20 })
		if hurt {
			break
		}
	}
	if !hurt {
		t.Fatal("wolf did not attack a skeleton five blocks away in 20 seconds")
	}
}

// TestTamedWolvesFollowTheirOwner checks FollowOwnerGoal: a tamed wolf more
// than ten blocks from its owner comes back to it.
func TestTamedWolvesFollowTheirOwner(t *testing.T) {
	w := wolfTestWorld(t)
	var wolf *world.EntityHandle
	ownerPos := mgl64.Vec3{11.5, 64, .5}
	<-w.Exec(func(tx *world.Tx) {
		p := addSurvivalPlayer(tx, ownerPos)
		wolf = addWolf(tx, mgl64.Vec3{.5, 64, .5}, entity.WolfBehaviourConfig{Tamed: true, OwnerUUID: p.UUID().String()})
	})
	closest := 100.0
	for range 200 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) { closest = min(closest, horizontalDistance(entityPos(tx, wolf), ownerPos)) })
	}
	if closest > 3 {
		t.Fatalf("tamed wolf came no closer than %.2f blocks to its owner", closest)
	}
}

// pen walls in the floor of the wolf test world from -radius to radius.
func pen(tx *world.Tx, radius int) {
	for i := -radius; i <= radius; i++ {
		for y := 64; y <= 65; y++ {
			tx.SetBlock(cube.Pos{i, y, -radius}, block.Stone{}, nil)
			tx.SetBlock(cube.Pos{i, y, radius}, block.Stone{}, nil)
			tx.SetBlock(cube.Pos{-radius, y, i}, block.Stone{}, nil)
			tx.SetBlock(cube.Pos{radius, y, i}, block.Stone{}, nil)
		}
	}
}

// TestTamedWolvesAttackWhatTheirOwnerHits checks OwnerHurtTargetGoal: a
// tamed wolf attacks the mob its owner hit.
func TestTamedWolvesAttackWhatTheirOwnerHits(t *testing.T) {
	w := wolfTestWorld(t)
	var cow *world.EntityHandle
	var owner *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		// The cow panics, but cannot run out of the pen.
		pen(tx, 8)
		// The owner stands aside: the wolf would not get past it, as entities
		// push each other apart.
		p := addSurvivalPlayer(tx, mgl64.Vec3{3.5, 64, -2.5})
		owner = p.H()
		addWolf(tx, mgl64.Vec3{.5, 64, 2.5}, entity.WolfBehaviourConfig{Tamed: true, OwnerUUID: p.UUID().String()})
		cow = spawnAnimal(t, tx, "minecraft:cow", 5.5, nil)
	})
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		p, _ := owner.Entity(tx)
		c, _ := cow.Entity(tx)
		if !p.(*player.Player).AttackEntity(c) {
			t.Fatal("owner could not hit the cow")
		}
	})
	var afterHit float64
	<-w.Exec(func(tx *world.Tx) { afterHit = livingHealth(tx, cow) })
	bitten := false
	for range 400 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) { bitten = livingHealth(tx, cow) < afterHit })
		if bitten {
			break
		}
	}
	if !bitten {
		t.Fatal("tamed wolf did not attack the cow its owner hit")
	}
}

// TestTamedWolvesDefendTheirOwner checks OwnerHurtByTargetGoal: a tamed wolf
// attacks the mob that hurt its owner.
func TestTamedWolvesDefendTheirOwner(t *testing.T) {
	w := wolfTestWorld(t)
	var cow, owner *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		// The owner stands aside: the wolf would not get past it, as entities
		// push each other apart.
		p := addSurvivalPlayer(tx, mgl64.Vec3{3.5, 64, -2.5})
		owner = p.H()
		addWolf(tx, mgl64.Vec3{.5, 64, 2.5}, entity.WolfBehaviourConfig{Tamed: true, OwnerUUID: p.UUID().String()})
		cow = spawnAnimal(t, tx, "minecraft:cow", 5.5, nil)
	})
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		p, _ := owner.Entity(tx)
		c, _ := cow.Entity(tx)
		p.(*player.Player).Hurt(1, entity.AttackDamageSource{Attacker: c})
	})
	bitten := false
	for range 200 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) { bitten = livingHealth(tx, cow) < 10 })
		if bitten {
			break
		}
	}
	if !bitten {
		t.Fatal("tamed wolf did not attack the mob that hurt its owner")
	}
}

// TestWolvesSitWhenOrdered checks SitWhenOrderedToGoal: a tamed wolf its
// owner orders to sit sits down and stays where it is.
func TestWolvesSitWhenOrdered(t *testing.T) {
	w := wolfTestWorld(t)
	var wolf, owner *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		p := addSurvivalPlayer(tx, mgl64.Vec3{2.5, 64, .5})
		owner = p.H()
		wolf = addWolf(tx, mgl64.Vec3{.5, 64, .5}, entity.WolfBehaviourConfig{Tamed: true, OwnerUUID: p.UUID().String()})
	})
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		p, _ := owner.Entity(tx)
		e, _ := wolf.Entity(tx)
		if !e.(*entity.Wolf).Interact(tx, p.(*player.Player), &item.UseContext{}) {
			t.Fatal("owner could not order the wolf to sit")
		}
	})
	for range 10 {
		w.AdvanceTick()
	}
	var start mgl64.Vec3
	<-w.Exec(func(tx *world.Tx) {
		e, _ := wolf.Entity(tx)
		if !e.(*entity.Wolf).Sitting() {
			t.Fatal("wolf ordered to sit does not sit")
		}
		start = e.Position()
		// The owner walks away; a sitting wolf stays.
		p, _ := owner.Entity(tx)
		p.(*player.Player).Teleport(mgl64.Vec3{15.5, 64, .5})
	})
	for range 100 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if d := horizontalDistance(entityPos(tx, wolf), start); d > .1 {
			t.Fatalf("sitting wolf moved %.2f blocks", d)
		}
	})
}

// TestHurtWolvesFightBack checks HurtByTargetGoal and NeutralMob for wolves:
// a wild wolf hurt by a player turns angry and bites it.
func TestHurtWolvesFightBack(t *testing.T) {
	w := wolfTestWorld(t)
	var wolf, attacker *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		p := addSurvivalPlayer(tx, mgl64.Vec3{3.5, 64, .5})
		attacker = p.H()
		wolf = addWolf(tx, mgl64.Vec3{.5, 64, .5}, entity.WolfBehaviourConfig{})
	})
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		p, _ := attacker.Entity(tx)
		e, _ := wolf.Entity(tx)
		e.(entity.Living).Hurt(1, entity.AttackDamageSource{Attacker: p})
	})
	lowest := 20.0
	angry := false
	for range 200 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			lowest = min(lowest, livingHealth(tx, attacker))
			e, _ := wolf.Entity(tx)
			angry = angry || e.(*entity.Wolf).Angry()
		})
	}
	if !angry {
		t.Fatal("wolf hurt by a player did not turn angry")
	}
	if lowest > 20-4+1e-9 {
		t.Fatalf("player who hurt a wolf kept %.2f health, want it bitten for 4", lowest)
	}
}

// TestWolvesBegForBones checks BegGoal: a wolf tilts its head at a player
// holding a bone.
func TestWolvesBegForBones(t *testing.T) {
	w := wolfTestWorld(t)
	var wolf *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		addSurvivalPlayer(tx, mgl64.Vec3{4.5, 64, .5}).SetHeldItems(item.NewStack(item.Bone{}, 1), item.Stack{})
		wolf = addWolf(tx, mgl64.Vec3{.5, 64, .5}, entity.WolfBehaviourConfig{})
	})
	interested := false
	for range 100 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			e, _ := wolf.Entity(tx)
			interested = interested || e.(*entity.Wolf).Interested()
		})
	}
	if !interested {
		t.Fatal("wolf did not beg at a player holding a bone")
	}
}
