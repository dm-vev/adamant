package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// TestMobEquipmentSurvivesSaving checks that every equipment slot and drop
// chance of a mob survives its native NBT.
func TestMobEquipmentSurvivesSaving(t *testing.T) {
	var data, restored world.EntityData
	HuskType.DecodeNBT(map[string]any{
		"equipment": map[string]any{
			"mainhand": map[string]any{"id": "minecraft:iron_sword", "count": int32(1)},
			"offhand":  map[string]any{"id": "minecraft:shield", "count": int32(1)},
			"feet":     map[string]any{"id": "minecraft:golden_boots", "count": int32(1)},
			"legs":     map[string]any{"id": "minecraft:chainmail_leggings", "count": int32(1)},
			"chest":    map[string]any{"id": "minecraft:iron_chestplate", "count": int32(1)},
			"head":     map[string]any{"id": "minecraft:leather_helmet", "count": int32(1)},
		},
		"drop_chances": map[string]any{"chest": float32(2), "head": float32(0)},
	}, &data)
	HuskType.DecodeNBT(HuskType.EncodeNBT(&data), &restored)
	b := restored.Data.(*hostileState)
	want := map[int]world.Item{
		slotMainhand: item.Sword{Tier: item.ToolTierIron}, slotOffhand: item.Shield{},
		slotFeet: item.Boots{Tier: item.ArmourTierGold{}}, slotLegs: item.Leggings{Tier: item.ArmourTierChain{}},
		slotChest: item.Chestplate{Tier: item.ArmourTierIron{}}, slotHead: item.Helmet{Tier: item.ArmourTierLeather{}},
	}
	for slot, it := range want {
		if got := b.equipment(slot); got.Empty() || got.Item() != it {
			t.Errorf("%s after reload holds %v, want %T", equipmentSlotNames[slot], got, it)
		}
	}
	if b.dropChance(slotChest) != 2 || b.dropChance(slotHead) != 0 || b.dropChance(slotLegs) != defaultDropChance {
		t.Fatalf("drop chances after reload: chest %v, head %v, legs %v; want 2, 0, %v", b.dropChance(slotChest), b.dropChance(slotHead), b.dropChance(slotLegs), defaultDropChance)
	}
}
