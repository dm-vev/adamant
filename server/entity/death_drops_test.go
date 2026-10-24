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

// deathDrops returns the experience of the orbs and the item stacks lying in
// the world.
func deathDrops(tx *world.Tx) (xp int, items []item.Stack) {
	for e := range tx.Entities() {
		ent, ok := e.(*entity.Ent)
		if !ok {
			continue
		}
		switch b := ent.Behaviour().(type) {
		case *entity.ExperienceOrbBehaviour:
			xp += b.Experience()
		case *entity.ItemBehaviour:
			items = append(items, b.Item())
		}
	}
	return xp, items
}

func newDeathTestWorld(t *testing.T) *world.World {
	w := world.Config{Entities: entity.DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func addDeathTestPlayer(tx *world.Tx) *player.Player {
	return tx.AddEntity((world.EntitySpawnOpts{}).New(player.Type, player.Config{Position: mgl64.Vec3{0.5, 65, 30.5}, GameMode: world.GameModeCreative})).(*player.Player)
}

func TestMobExperienceRequiresPlayerKillAndFollowsJavaRewards(t *testing.T) {
	cases := []struct {
		name           string
		nbt            map[string]any
		byPlayer       bool
		minXP, maxXP   int
		wantItemsEmpty bool
	}{
		{name: "adult zombie killed by player", nbt: map[string]any{"id": "minecraft:zombie"}, byPlayer: true, minXP: 5, maxXP: 5},
		{name: "baby zombie rewards 2.5 times", nbt: map[string]any{"id": "minecraft:zombie", "IsBaby": byte(1)}, byPlayer: true, minXP: 12, maxXP: 12},
		{name: "zombie killed by a fall", nbt: map[string]any{"id": "minecraft:zombie"}},
		{name: "adult cow killed by player", nbt: map[string]any{"id": "minecraft:cow"}, byPlayer: true, minXP: 1, maxXP: 3},
		{name: "calf drops neither loot nor experience", nbt: map[string]any{"id": "minecraft:cow", "Age": int32(-24000)}, byPlayer: true, wantItemsEmpty: true},
		{name: "blaze rewards ten", nbt: map[string]any{"id": "minecraft:blaze"}, byPlayer: true, minXP: 10, maxXP: 10},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newDeathTestWorld(t)
			<-w.Exec(func(tx *world.Tx) {
				var src world.DamageSource = entity.FallDamageSource{}
				if c.byPlayer {
					src = entity.AttackDamageSource{Attacker: addDeathTestPlayer(tx)}
				}
				h, err := entity.NewFromStructureNBT(c.nbt, world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}, SpawnFinalized: true})
				if err != nil {
					t.Fatal(err)
				}
				mob := tx.AddEntity(h).(entity.Living)
				mob.Hurt(1000, src)
				if !mob.Dead() {
					t.Fatal("mob survived lethal damage")
				}
				xp, items := deathDrops(tx)
				for dropped := range tx.Entities() {
					if dropped.H().Type() != entity.ItemType {
						continue
					}
					v := dropped.(*entity.Ent).Velocity()
					if v[1] != 0.2 || v[0] < -0.1 || v[0] >= 0.1 || v[2] < -0.1 || v[2] >= 0.1 {
						t.Errorf("death drop motion=%v, want upward toss and horizontal spread", v)
					}
				}
				if xp < c.minXP || xp > c.maxXP {
					t.Fatalf("experience = %d, want %d..%d", xp, c.minXP, c.maxXP)
				}
				if c.wantItemsEmpty && len(items) != 0 {
					t.Fatalf("baby dropped %v", items)
				}
			})
		})
	}
}

func TestSculkCatalystAbsorbsDeathExperienceAndSpreadsSculk(t *testing.T) {
	w := newDeathTestWorld(t)
	catalyst := cube.Pos{0, 64, 0}
	<-w.Exec(func(tx *world.Tx) {
		for x := -12; x <= 12; x++ {
			for z := -12; z <= 12; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Dirt{}, nil)
			}
		}
		tx.SetBlock(catalyst, block.NewSculkCatalyst(), nil)
		zombie := tx.AddEntity(entity.NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{6.5, 64, 0.5}})).(entity.Living)
		zombie.Hurt(1000, entity.AttackDamageSource{Attacker: addDeathTestPlayer(tx)})
		if xp, _ := deathDrops(tx); xp != 0 {
			t.Fatalf("catalyst left %d experience on the ground", xp)
		}
		if c, ok := tx.Block(catalyst).(block.SculkCatalyst); !ok || !c.Bloom {
			t.Fatalf("catalyst did not bloom: %#v", tx.Block(catalyst))
		}
	})
	for range 200 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if c, ok := tx.Block(catalyst).(block.SculkCatalyst); !ok || c.Bloom {
			t.Fatalf("catalyst still blooming after its scheduled tick: %#v", tx.Block(catalyst))
		}
		sculk := 0
		for x := -12; x <= 12; x++ {
			for z := -12; z <= 12; z++ {
				if _, ok := tx.Block(cube.Pos{x, 63, z}).(block.Sculk); ok {
					sculk++
				}
			}
		}
		if sculk == 0 {
			t.Fatal("the absorbed experience spread no sculk")
		}
	})
}

func TestCatalystOutOfRangeLeavesExperience(t *testing.T) {
	w := newDeathTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 64, 0}, block.NewSculkCatalyst(), nil)
		zombie := tx.AddEntity(entity.NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{9.5, 64, 0.5}})).(entity.Living)
		zombie.Hurt(1000, entity.AttackDamageSource{Attacker: addDeathTestPlayer(tx)})
		if xp, _ := deathDrops(tx); xp != 5 {
			t.Fatalf("experience = %d, want the zombie's 5 beyond the 8 block radius", xp)
		}
		if c := tx.Block(cube.Pos{0, 64, 0}).(block.SculkCatalyst); c.Bloom {
			t.Fatal("catalyst bloomed for a death outside its radius")
		}
	})
}

func TestMountDeathDropsEquipmentAndChest(t *testing.T) {
	w := newDeathTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		h, err := entity.NewFromStructureNBT(map[string]any{"id": "minecraft:donkey", "Tame": byte(1)}, world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}, SpawnFinalized: true})
		if err != nil {
			t.Fatal(err)
		}
		donkey := tx.AddEntity(h).(*entity.Donkey)
		owner := addDeathTestPlayer(tx)
		owner.SetHeldItems(item.NewStack(block.Chest{}, 1), item.Stack{})
		donkey.Interact(tx, owner, &item.UseContext{})
		owner.SetHeldItems(item.NewStack(item.Saddle{}, 1), item.Stack{})
		donkey.Interact(tx, owner, &item.UseContext{})
		if !donkey.Saddled() || !donkey.Chested() || donkey.InventorySize() != 15 {
			t.Fatalf("fixture donkey saddled=%t chested=%t slots=%d, want a saddled donkey with a 15 slot chest", donkey.Saddled(), donkey.Chested(), donkey.InventorySize())
		}
		donkey.SetInventoryItem(14, item.NewStack(item.Diamond{}, 3))
		donkey.Hurt(1000, entity.FallDamageSource{})
		if _, ok := donkey.H().Entity(tx); ok {
			t.Fatal("dead donkey is still in the world")
		}
		counts := map[string]int{}
		_, items := deathDrops(tx)
		for _, stack := range items {
			name, _ := stack.Item().EncodeItem()
			counts[name] += stack.Count()
		}
		if counts["minecraft:saddle"] != 1 || counts["minecraft:chest"] != 1 || counts["minecraft:diamond"] != 3 {
			t.Fatalf("drops = %v, want saddle, chest and the 3 stored diamonds", counts)
		}
	})
}
