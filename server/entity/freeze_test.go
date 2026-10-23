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

// buildPowderSnowPit fills a roofed 5x5 pit two blocks deep with powder snow
// on a stone floor at y=63.
func buildPowderSnowPit(tx *world.Tx) {
	for x := -2; x <= 2; x++ {
		for z := -2; z <= 2; z++ {
			tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			tx.SetBlock(cube.Pos{x, 64, z}, block.PowderSnow{}, nil)
			tx.SetBlock(cube.Pos{x, 65, z}, block.PowderSnow{}, nil)
			tx.SetBlock(cube.Pos{x, 67, z}, block.Stone{}, nil)
		}
	}
}

func TestMobFreezesInPowderSnowAndTakesFreezeDamage(t *testing.T) {
	w := newDeathTestWorld(t)
	var h *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		buildPowderSnowPit(tx)
		h = tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}})).H()
	})
	cowIn := func(tx *world.Tx) *entity.Cow {
		e, ok := h.Entity(tx)
		if !ok {
			t.Fatal("cow left the world")
		}
		return e.(*entity.Cow)
	}
	for range 139 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		cow := cowIn(tx)
		if cow.FrozenTicks() >= 140 || cow.FrozenTicks() < 130 {
			t.Fatalf("frozen ticks after 139 ticks = %d, want one per tick", cow.FrozenTicks())
		}
		if cow.Health() != cow.MaxHealth() {
			t.Fatalf("cow took damage before freezing fully: %v", cow.Health())
		}
	})
	for range 81 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		cow := cowIn(tx)
		if cow.FrozenTicks() != 140 {
			t.Fatalf("frozen ticks = %d, want the 140 cap", cow.FrozenTicks())
		}
		if lost := cow.MaxHealth() - cow.Health(); lost < 1 || lost > 3 {
			t.Fatalf("fully frozen cow lost %v health in 80 ticks, want one per 40", lost)
		}
		// Thawing outside the snow takes two ticks per tick.
		cow.Teleport(mgl64.Vec3{10.5, 64, 10.5})
	})
	for range 10 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if got := cowIn(tx).FrozenTicks(); got != 120 {
			t.Fatalf("frozen ticks after thawing 10 ticks = %d, want 120", got)
		}
	})
}

func TestLeatherArmourPreventsFreezing(t *testing.T) {
	w := newDeathTestWorld(t)
	var zombie *entity.Zombie
	<-w.Exec(func(tx *world.Tx) {
		buildPowderSnowPit(tx)
		h, err := entity.NewFromStructureNBT(map[string]any{"id": "minecraft:zombie", "equipment": map[string]any{"head": map[string]any{"id": "minecraft:leather_helmet", "count": int32(1)}}}, world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}, SpawnFinalized: true})
		if err != nil {
			t.Fatal(err)
		}
		zombie = tx.AddEntity(h).(*entity.Zombie)
	})
	for range 60 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if got := zombie.FrozenTicks(); got != 0 {
			t.Fatalf("zombie in a leather helmet froze %d ticks", got)
		}
	})
}

func TestSkeletonFreezesIntoStray(t *testing.T) {
	w := newDeathTestWorld(t)
	var skeleton *entity.Skeleton
	<-w.Exec(func(tx *world.Tx) {
		buildPowderSnowPit(tx)
		skeleton = tx.AddEntity(entity.NewSkeleton(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}})).(*entity.Skeleton)
	})
	for range 400 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if !skeleton.Shaking() || skeleton.FrozenTicks() != 0 {
			t.Fatalf("skeleton after 400 ticks shaking=%t frozen=%d, want shaking without freezing", skeleton.Shaking(), skeleton.FrozenTicks())
		}
	})
	for range 60 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if _, ok := skeleton.H().Entity(tx); ok {
			t.Fatal("skeleton still exists 460 ticks into powder snow")
		}
		strays := 0
		for e := range tx.Entities() {
			if e.H().Type().EncodeEntity() == "minecraft:stray" {
				strays++
			}
		}
		if strays != 1 {
			t.Fatalf("found %d strays, want the converted skeleton", strays)
		}
	})
}

func TestFreezeDamageGameRuleProtectsPlayers(t *testing.T) {
	w := newDeathTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		p := tx.AddEntity((world.EntitySpawnOpts{}).New(player.Type, player.Config{Position: mgl64.Vec3{0.5, 64, 0.5}, GameMode: world.GameModeSurvival})).(*player.Player)
		w.SetGameRule("freeze_damage", false)
		if dealt, ok := p.Hurt(2, entity.FreezeDamageSource{}); ok || dealt != 0 {
			t.Fatalf("freeze damage with the rule off dealt %v", dealt)
		}
		w.SetGameRule("freeze_damage", true)
		if dealt, ok := p.Hurt(2, entity.FreezeDamageSource{}); !ok || dealt != 2 {
			t.Fatalf("freeze damage with the rule on dealt %v, %t", dealt, ok)
		}
	})
}
