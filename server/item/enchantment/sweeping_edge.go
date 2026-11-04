package enchantment

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// SweepingEdge is a Java enchantment of swords that raises the damage of
// a sweep attack: it adds level/(level+1) to the sweeping damage ratio
// (data/minecraft/enchantment/sweeping_edge.json). Bedrock has no Sweeping
// Edge: it is a Java-only enchantment (item.RegisterJavaOnlyEnchantment),
// which item NBT holds by its Java ID, minecraft:sweeping_edge.
var SweepingEdge sweepingEdge

type sweepingEdge struct{}

// Name ...
func (sweepingEdge) Name() string {
	return "Sweeping Edge"
}

// MaxLevel ...
func (sweepingEdge) MaxLevel() int {
	return 3
}

// Cost ...
func (sweepingEdge) Cost(level int) (int, int) {
	minCost := 5 + (level-1)*9
	return minCost, minCost + 15
}

// Rarity ...
func (sweepingEdge) Rarity() item.EnchantmentRarity {
	return item.EnchantmentRarityRare
}

// DamageRatio returns the share of the damage of a hit that a sweep
// attack deals the entities around its target, from an enchantment of the
// level: 1/2, 2/3 and 3/4.
func (sweepingEdge) DamageRatio(level int) float64 {
	return float64(level) / float64(level+1)
}

// CompatibleWithEnchantment ...
func (sweepingEdge) CompatibleWithEnchantment(item.EnchantmentType) bool {
	return true
}

// CompatibleWithItem reports if i is a sword, #enchantable/sweeping, the
// supported_items of data/minecraft/enchantment/sweeping_edge.json. A spear
// is not one.
func (sweepingEdge) CompatibleWithItem(i world.Item) bool {
	t, ok := i.(item.Tool)
	return ok && t.ToolType() == item.TypeSword
}
