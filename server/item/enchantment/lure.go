package enchantment

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Lure decreases the waiting time before something bites on a fishing rod.
var Lure lure

type lure struct{}

// Name ...
func (lure) Name() string {
	return "Lure"
}

// MaxLevel ...
func (lure) MaxLevel() int {
	return 3
}

// Cost ...
func (lure) Cost(level int) (int, int) {
	minCost := 15 + (level-1)*9
	return minCost, minCost + 50
}

// Rarity ...
func (lure) Rarity() item.EnchantmentRarity {
	return item.EnchantmentRarityRare
}

// CompatibleWithEnchantment ...
func (lure) CompatibleWithEnchantment(item.EnchantmentType) bool {
	return true
}

// CompatibleWithItem ...
func (lure) CompatibleWithItem(i world.Item) bool {
	_, ok := i.(item.FishingRod)
	return ok
}
