package menu_test

import (
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
)

// TestEnchantingTableOffers checks the enchantments an enchanting table may
// offer for an item against Java 26.3. EnchantmentHelper's
// getAvailableEnchantmentResults takes those of #in_enchanting_table, the
// non-treasure ones of data/minecraft/tags/enchantment/non_treasure.json,
// whose primary_items hold the item, or all of them for a book. The primary
// items may be fewer than those an anvil or /enchant put an enchantment on:
// Smite and Fire Aspect go on a mace and Sharpness on an axe, but a table
// offers them for a sword only.
func TestEnchantingTableOffers(t *testing.T) {
	diamond := item.ArmourTierDiamond{}
	protections := []item.EnchantmentType{enchantment.Protection, enchantment.FireProtection, enchantment.BlastProtection, enchantment.ProjectileProtection}
	nonTreasure := []item.EnchantmentType{
		enchantment.Protection, enchantment.FireProtection, enchantment.FeatherFalling, enchantment.BlastProtection,
		enchantment.ProjectileProtection, enchantment.Respiration, enchantment.AquaAffinity, enchantment.Thorns,
		enchantment.DepthStrider, enchantment.Sharpness, enchantment.Smite, enchantment.BaneOfArthropods,
		enchantment.Knockback, enchantment.FireAspect, enchantment.Looting, enchantment.SweepingEdge, enchantment.Efficiency,
		enchantment.SilkTouch, enchantment.Unbreaking, enchantment.Fortune, enchantment.Power, enchantment.Punch,
		enchantment.Flame, enchantment.Infinity, enchantment.LuckOfTheSea, enchantment.Lure, enchantment.Loyalty,
		enchantment.Impaling, enchantment.Riptide, enchantment.Channeling, enchantment.Multishot,
		enchantment.QuickCharge, enchantment.Piercing, enchantment.Density, enchantment.Breach, enchantment.Lunge,
	}
	for _, tc := range []struct {
		name string
		it   world.Item
		want []item.EnchantmentType
	}{
		// Wind Burst and Mending are treasure; Smite, Bane of Arthropods and
		// Fire Aspect have the melee weapons as primary items.
		{"mace", item.Mace{}, []item.EnchantmentType{enchantment.Unbreaking, enchantment.Density, enchantment.Breach}},
		{"sword", item.Sword{Tier: item.ToolTierDiamond}, []item.EnchantmentType{
			enchantment.Sharpness, enchantment.Smite, enchantment.BaneOfArthropods, enchantment.Knockback,
			enchantment.FireAspect, enchantment.Looting, enchantment.SweepingEdge, enchantment.Unbreaking,
		}},
		// #enchantable/melee_weapon holds the spears.
		{"spear", item.Spear{Tier: item.ToolTierDiamond}, []item.EnchantmentType{
			enchantment.Sharpness, enchantment.Smite, enchantment.BaneOfArthropods, enchantment.Knockback,
			enchantment.FireAspect, enchantment.Looting, enchantment.Unbreaking, enchantment.Lunge,
		}},
		{"axe", item.Axe{Tier: item.ToolTierDiamond}, []item.EnchantmentType{
			enchantment.Efficiency, enchantment.SilkTouch, enchantment.Fortune, enchantment.Unbreaking,
		}},
		// Thorns has chest armour as primary items.
		{"chestplate", item.Chestplate{Tier: diamond}, append(slices.Clone(protections), enchantment.Thorns, enchantment.Unbreaking)},
		{"boots", item.Boots{Tier: diamond}, append(slices.Clone(protections), enchantment.FeatherFalling, enchantment.DepthStrider, enchantment.Unbreaking)},
		{"book", item.Book{}, nonTreasure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got, want []string
			for _, e := range item.Enchantments() {
				if menu.OfferedByTable(e, tc.it) {
					got = append(got, e.Name())
				}
			}
			for _, e := range tc.want {
				want = append(want, e.Name())
			}
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("offers %v, want %v", got, want)
			}
		})
	}
}
