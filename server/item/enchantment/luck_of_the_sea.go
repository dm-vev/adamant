package enchantment

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// LuckOfTheSea increases the chance to obtain treasure while fishing.
var LuckOfTheSea luckOfTheSea

type luckOfTheSea struct{}

// Name ...
func (luckOfTheSea) Name() string {
	return "Luck of the Sea"
}

// MaxLevel ...
func (luckOfTheSea) MaxLevel() int {
	return 3
}

// Cost ...
func (luckOfTheSea) Cost(level int) (int, int) {
	minCost := 15 + (level-1)*9
	return minCost, minCost + 50
}

// Rarity ...
func (luckOfTheSea) Rarity() item.EnchantmentRarity {
	return item.EnchantmentRarityRare
}

// CompatibleWithEnchantment ...
func (luckOfTheSea) CompatibleWithEnchantment(item.EnchantmentType) bool {
	return true
}

// CompatibleWithItem ...
func (luckOfTheSea) CompatibleWithItem(i world.Item) bool {
	_, ok := i.(item.FishingRod)
	return ok
}
