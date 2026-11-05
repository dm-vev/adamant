package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
)

// TestVillagerEquipmentGetsOnlyPrimaryEnchantments checks that a traded item
// is enchanted with what an enchanting table offers it, as Java's
// EnchantmentHelper.getAvailableEnchantmentResults keeps only the enchantments
// for which isPrimaryItem holds. An iron axe takes Smite at an anvil, but a
// table and a villager never give it.
func TestVillagerEquipmentGetsOnlyPrimaryEnchantments(t *testing.T) {
	t.Parallel()
	assets := loadVillagerTradeAssets()
	stacks := []item.Stack{
		item.NewStack(item.Axe{Tier: item.ToolTierIron}, 1),
		item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1),
		item.NewStack(item.Pickaxe{Tier: item.ToolTierIron}, 1),
	}
	for _, base := range stacks {
		enchanted := 0
		for seed := int64(0); seed < 3000; seed++ {
			random := vanilla.NewLegacyRandom(seed)
			stack, ok := enchantVillagerEquipment(base, 5+int(seed%30), "minecraft:on_traded_equipment", assets, &random)
			if !ok {
				continue
			}
			enchanted++
			for _, e := range stack.Enchantments() {
				if !item.IsPrimaryItem(e.Type(), base.Item()) {
					t.Fatalf("%T got %s, which a table does not offer it (seed %d)", base.Item(), e.Type().Name(), seed)
				}
			}
		}
		if enchanted == 0 {
			t.Fatalf("%T was never enchanted", base.Item())
		}
	}
}
