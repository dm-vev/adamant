package entity_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func addParrot(tx *world.Tx, pos mgl64.Vec3, conf entity.ParrotConfig) *world.EntityHandle {
	return tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(entity.ParrotType, conf)).H()
}

// TestParrotsPerchOnTrees checks ParrotWanderGoal: a parrot on a canopy
// mostly flies to another free block on the leaves and lands there.
func TestParrotsPerchOnTrees(t *testing.T) {
	w := wolfTestWorld(t)
	var parrot *world.EntityHandle
	start := cube.Pos{2, 65, 0}
	<-w.Exec(func(tx *world.Tx) {
		for x := 0; x <= 4; x++ {
			for z := -2; z <= 2; z++ {
				tx.SetBlock(cube.Pos{x, 64, z}, block.Leaves{Type: block.OakLeaves(), Persistent: true}, nil)
			}
		}
		parrot = addParrot(tx, start.Vec3Middle(), entity.ParrotConfig{})
		// Mobs idle without a player within 32 blocks.
		addSurvivalPlayer(tx, mgl64.Vec3{-15.5, 64, .5})
	})
	for range 1200 {
		w.AdvanceTick()
		perched := false
		<-w.Exec(func(tx *world.Tx) {
			e, _ := parrot.Entity(tx)
			pos := e.Position()
			_, onLeaves := tx.Block(cube.PosFromVec3(pos).Side(cube.FaceDown)).(block.Leaves)
			perched = cube.PosFromVec3(pos) != start && onLeaves && pos[1]-65 < .01 && e.(*entity.Parrot).OnGround()
		})
		if perched {
			return
		}
	}
	t.Fatal("parrot did not fly to another perch on the canopy in a minute")
}

// TestParrotsFlapToFallSlowly checks FlyingMoveControl and
// Parrot.calculateFlapping: a parrot with nowhere to fly to falls with
// gravity, its flapping keeping it slower than a tenth of a block a tick.
func TestParrotsFlapToFallSlowly(t *testing.T) {
	w := wolfTestWorld(t)
	var parrot *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		parrot = addParrot(tx, mgl64.Vec3{.5, 80, .5}, entity.ParrotConfig{})
	})
	fastest, landed := 0.0, false
	for range 400 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			e, _ := parrot.Entity(tx)
			p := e.(*entity.Parrot)
			fastest = math.Max(fastest, -p.Velocity()[1])
			landed = landed || p.OnGround()
		})
	}
	if fastest > .11 {
		t.Fatalf("parrot fell %.3f blocks a tick, want at most about 0.1", fastest)
	}
	if !landed {
		t.Fatal("parrot did not come down in 20 seconds")
	}
}

// TestParrotsLandOnTheirOwnersShoulder checks LandOnOwnersShoulderGoal and
// ServerPlayer.setEntityOnShoulder: once five seconds in the world, a tamed
// parrot its owner on the ground touches sits on the owner's left shoulder.
func TestParrotsLandOnTheirOwnersShoulder(t *testing.T) {
	w := wolfTestWorld(t)
	var parrot, owner *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		p := addSurvivalPlayer(tx, mgl64.Vec3{4.5, 64, .5})
		owner = p.H()
		parrot = addParrot(tx, mgl64.Vec3{.5, 64, .5}, entity.ParrotConfig{Tame: true, Owner: p.UUID().String()})
	})
	for i := range 1200 {
		w.AdvanceTick()
		sat := false
		<-w.Exec(func(tx *world.Tx) {
			o, _ := owner.Entity(tx)
			left, _ := o.(interface {
				ShoulderEntities() (map[string]any, map[string]any)
			}).ShoulderEntities()
			e, ok := parrot.Entity(tx)
			if left != nil {
				if ok {
					t.Fatal("parrot sat on its owner's shoulder but stayed in the world")
				}
				if i < 100 {
					t.Fatalf("parrot sat on its owner's shoulder after %d ticks, want over 100", i)
				}
				sat = true
				return
			}
			if ok && i > 100 && i%20 == 1 {
				// The owner steps into the parrot now and then, as a player
				// walking up to it. Stepping in every tick would push the
				// parrot away.
				pos := e.Position()
				o.(interface{ Teleport(mgl64.Vec3) }).Teleport(mgl64.Vec3{pos[0], 64, pos[2]})
			}
		})
		if sat {
			return
		}
	}
	t.Fatal("tamed parrot did not land on its owner's shoulder in a minute")
}

// TestParrotsFollowMobs checks FollowMobGoal: a parrot flies after a mob of
// another species nearby.
func TestParrotsFollowMobs(t *testing.T) {
	w := wolfTestWorld(t)
	var parrot, cow *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		// The pen keeps the cow within seven blocks of the parrot.
		pen(tx, 7)
		parrot = addParrot(tx, mgl64.Vec3{-.5, 64, .5}, entity.ParrotConfig{})
		cow = spawnAnimal(t, tx, "minecraft:cow", 5.5, nil)
		// Mobs idle without a player within 32 blocks, and a parrot that
		// wandered off would stay away from the cow.
		addSurvivalPlayer(tx, mgl64.Vec3{-17.5, 64, .5})
	})
	closest := 100.0
	for range 1200 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			closest = min(closest, horizontalDistance(entityPos(tx, parrot), entityPos(tx, cow)))
		})
		if closest < 3.5 {
			return
		}
	}
	t.Fatalf("parrot came no closer than %.2f blocks to the cow nearby", closest)
}
