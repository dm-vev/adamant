package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func addBee(tx *world.Tx, pos mgl64.Vec3, conf entity.BeeConfig) *world.EntityHandle {
	return tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(entity.BeeType, conf)).H()
}

func beeOf(tx *world.Tx, h *world.EntityHandle) (*entity.Bee, bool) {
	e, ok := h.Entity(tx)
	if !ok {
		return nil, false
	}
	return e.(*entity.Bee), true
}

// TestBeesPollinateFlowers checks BeePollinateGoal: a bee in a field of
// flowers flies to one and gathers nectar after hovering over it for 20
// seconds.
func TestBeesPollinateFlowers(t *testing.T) {
	w := traderTestWorld(t)
	var bee *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		for x := -12; x <= 12; x += 3 {
			for z := -12; z <= 12; z += 3 {
				tx.SetBlock(cube.Pos{x, 64, z}, block.Flower{Type: block.Dandelion()}, nil)
			}
		}
		bee = addBee(tx, mgl64.Vec3{.5, 65, .5}, entity.BeeConfig{})
	})
	for i := range 2400 {
		w.AdvanceTick()
		nectar := false
		<-w.Exec(func(tx *world.Tx) {
			b, _ := beeOf(tx, bee)
			nectar = b.HasNectar()
		})
		if nectar {
			if i < 400 {
				t.Fatalf("bee gathered nectar after %d ticks, want over 400", i)
			}
			return
		}
	}
	t.Fatal("bee did not gather nectar from a flower nearby in two minutes")
}

// TestBeesTakeNectarHome checks BeeLocateHiveGoal, BeeGoToHiveGoal and
// BeeEnterHiveGoal: a bee carrying nectar finds a hive nearby, flies to it
// and enters it.
func TestBeesTakeNectarHome(t *testing.T) {
	w := traderTestWorld(t)
	var bee *world.EntityHandle
	hive := cube.Pos{4, 64, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(hive, block.Beehive{Facing: cube.West}, nil)
		bee = addBee(tx, mgl64.Vec3{-2.5, 65, .5}, entity.BeeConfig{HasNectar: true})
	})
	for range 1200 {
		w.AdvanceTick()
		entered := false
		<-w.Exec(func(tx *world.Tx) {
			if _, ok := beeOf(tx, bee); ok {
				return
			}
			occupants := tx.Block(hive).(block.Beehive).Occupants
			if len(occupants) != 1 {
				t.Fatalf("bee left the world, hive holds %d bees, want 1", len(occupants))
			}
			entered = true
		})
		if entered {
			return
		}
	}
	t.Fatal("bee carrying nectar did not enter the hive nearby in a minute")
}

// TestBeesStingWhatHurtThem checks BeeHurtByOtherGoal, BeeAttackGoal and
// Bee.doHurtTarget: a hurt bee stings its attacker, poisoning it, and dies
// within a minute of losing its stinger.
func TestBeesStingWhatHurtThem(t *testing.T) {
	w := traderTestWorld(t)
	var bee, player *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		tx.World().SetDifficulty(world.DifficultyNormal)
		p := addSurvivalPlayer(tx, mgl64.Vec3{3.5, 64, .5})
		player = p.H()
		bee = addBee(tx, mgl64.Vec3{.5, 65, .5}, entity.BeeConfig{})
		b, _ := beeOf(tx, bee)
		b.Hurt(1, entity.AttackDamageSource{Attacker: p})
	})
	stung := false
	for range 400 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			b, _ := beeOf(tx, bee)
			stung = b.HasStung()
		})
		if stung {
			break
		}
	}
	if !stung {
		t.Fatal("hurt bee did not sting its attacker in 20 seconds")
	}
	<-w.Exec(func(tx *world.Tx) {
		p, _ := player.Entity(tx)
		if !hasEffect(p.(entity.Living), effect.Poison) {
			t.Fatal("stung player is not poisoned on Normal difficulty")
		}
	})
	for range 1201 {
		w.AdvanceTick()
		alive := true
		<-w.Exec(func(tx *world.Tx) { _, alive = beeOf(tx, bee) })
		if !alive {
			return
		}
	}
	t.Fatal("bee lived over a minute after losing its stinger")
}

// TestHarvestingHoneyAngersBees checks BeehiveBlock.useItemOn: bottling the
// honey of a hive without smoke makes the bees around attack and releases
// those inside at the player.
func TestHarvestingHoneyAngersBees(t *testing.T) {
	w := traderTestWorld(t)
	hive := cube.Pos{2, 64, 0}
	<-w.Exec(func(tx *world.Tx) {
		occupant := map[string]any{"ActorIdentifier": "minecraft:bee", "SaveData": entity.BeeType.EncodeNBT(&world.EntityData{Data: entity.BeeConfig{}.New()}), "TicksLeftToStay": int32(600)}
		tx.SetBlock(hive, block.Beehive{Facing: cube.West, HoneyLevel: 5, Occupants: []any{occupant}}, nil)
		addBee(tx, mgl64.Vec3{6.5, 65, 4.5}, entity.BeeConfig{})
		p := addSurvivalPlayer(tx, mgl64.Vec3{.5, 64, .5})
		p.SetHeldItems(item.NewStack(item.GlassBottle{}, 1), item.Stack{})
		ctx := &item.UseContext{}
		if !(item.GlassBottle{}).UseOnBlock(hive, cube.FaceWest, mgl64.Vec3{}, tx, p, ctx) {
			t.Fatal("glass bottle did not take the honey of a full hive")
		}
		if h := tx.Block(hive).(block.Beehive); h.HoneyLevel != 0 || len(h.Occupants) != 0 {
			t.Fatalf("hive holds honey %d and %d bees after bottling, want none of either", h.HoneyLevel, len(h.Occupants))
		}
		bees := 0
		for e := range tx.Entities() {
			b, ok := e.(*entity.Bee)
			if !ok {
				continue
			}
			bees++
			if b.Target() == nil || b.Target().H() != p.H() {
				t.Errorf("bee at %v does not attack the player that took the honey", b.Position())
			}
		}
		if bees != 2 {
			t.Fatalf("found %d bees, want the released one and the one outside", bees)
		}
	})
}
