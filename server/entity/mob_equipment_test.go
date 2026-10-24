package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func husk(tx *world.Tx, h *world.EntityHandle) *entity.HostileMob {
	e, _ := h.Entity(tx)
	return e.(*entity.Husk).HostileMob
}

// TestMobsPickUpAndWearLoot checks Mob.aiStep and equipItemIfPossible: a
// husk that may pick up loot puts on a helmet and takes up a sword lying at
// its feet, and swaps the sword for a better one.
func TestMobsPickUpAndWearLoot(t *testing.T) {
	w := happyGhastWorld(t)
	var h *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		h = spawnAnimal(t, tx, "minecraft:husk", .5, map[string]any{"CanPickUpLoot": byte(1), "PersistenceRequired": byte(1)})
		for _, s := range []item.Stack{item.NewStack(item.Helmet{Tier: item.ArmourTierIron{}}, 1), item.NewStack(item.Sword{Tier: item.ToolTierStone}, 1)} {
			tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 64, .5}}, s))
		}
	})
	for range 30 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		m := husk(tx, h)
		head, _, _, _ := m.EquipmentArmour()
		main, _ := m.HeldItems()
		if _, ok := head.Item().(item.Helmet); !ok {
			t.Fatalf("husk standing on a helmet wears %v", head)
		}
		if s, ok := main.Item().(item.Sword); !ok || s.Tier != item.ToolTierStone {
			t.Fatalf("husk standing on a stone sword holds %v", main)
		}
		tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: m.Position()}, item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1)))
	})
	for range 30 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		main, _ := husk(tx, h).HeldItems()
		if s, ok := main.Item().(item.Sword); !ok || s.Tier != item.ToolTierDiamond {
			t.Fatalf("husk offered a diamond sword for its stone one holds %v", main)
		}
	})
}

// TestMobArmourAbsorbsDamage checks LivingEntity.getDamageAfterArmorAbsorb
// for a mob: a husk in diamond armour loses less health to a blow than one
// without.
func TestMobArmourAbsorbsDamage(t *testing.T) {
	w := happyGhastWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		bare := husk(tx, spawnAnimal(t, tx, "minecraft:husk", .5, nil))
		armoured := husk(tx, spawnAnimal(t, tx, "minecraft:husk", 4.5, map[string]any{"equipment": map[string]any{
			"chest": map[string]any{"id": "minecraft:diamond_chestplate", "count": int32(1)},
			"legs":  map[string]any{"id": "minecraft:diamond_leggings", "count": int32(1)},
		}}))
		b, _ := bare.Hurt(10, entity.AttackDamageSource{})
		a, _ := armoured.Hurt(10, entity.AttackDamageSource{})
		// Base armour 2: 10 * (1 - max(2-10/2, .4)/25) = 9.84; with 14 more
		// from the diamond pieces and 4 toughness: 10 * (1 - (16-10/3)/25).
		if b < 9.8 || b > 9.9 {
			t.Fatalf("bare husk took %v of a 10 damage blow, want 9.84", b)
		}
		if want := 10 * (1 - (16-10.0/3)/25); a < want-1e-6 || a > want+1e-6 {
			t.Fatalf("husk in diamond took %v of a 10 damage blow, want %v", a, want)
		}
	})
}
