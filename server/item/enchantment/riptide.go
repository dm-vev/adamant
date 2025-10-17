package enchantment

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Riptide propels the wielder forward when throwing a trident in water or rain.
var Riptide riptide

type riptide struct{}

// Name ...
func (riptide) Name() string {
	return "Riptide"
}

// MaxLevel ...
func (riptide) MaxLevel() int {
	return 3
}

// Cost ...
func (riptide) Cost(level int) (int, int) {
	minCost := 17 + (level-1)*7
	return minCost, minCost + 50
}

// Rarity ...
func (riptide) Rarity() item.EnchantmentRarity {
	return item.EnchantmentRarityRare
}

// CompatibleWithEnchantment ...
func (riptide) CompatibleWithEnchantment(t item.EnchantmentType) bool {
	_, isLoyalty := t.(loyalty)
	_, isChanneling := t.(channeling)
	return !isLoyalty && !isChanneling
}

// CompatibleWithItem ...
func (riptide) CompatibleWithItem(i world.Item) bool {
	_, ok := i.(item.Trident)
	return ok
}

// LaunchVelocity returns the launch speed of a riptide trident at the given level.
func (riptide) LaunchVelocity(level int) float64 {
	switch level {
	case 3:
		return 5
	case 2:
		return 4
	default:
		return 3
	}
}
