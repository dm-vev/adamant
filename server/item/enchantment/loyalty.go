package enchantment

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Loyalty is a trident enchantment that causes thrown tridents to return to their wielder.
var Loyalty loyalty

type loyalty struct{}

// Name ...
func (loyalty) Name() string {
	return "Loyalty"
}

// MaxLevel ...
func (loyalty) MaxLevel() int {
	return 3
}

// Cost ...
func (loyalty) Cost(level int) (int, int) {
	minCost := 12 + (level-1)*7
	return minCost, minCost + 50
}

// Rarity ...
func (loyalty) Rarity() item.EnchantmentRarity {
	return item.EnchantmentRarityRare
}

// CompatibleWithEnchantment ...
func (loyalty) CompatibleWithEnchantment(t item.EnchantmentType) bool {
	_, isRiptide := t.(riptide)
	return !isRiptide
}

// CompatibleWithItem ...
func (loyalty) CompatibleWithItem(i world.Item) bool {
	_, ok := i.(item.Trident)
	return ok
}

// ReturnSpeed returns the base return speed of a trident with the given loyalty level.
func (loyalty) ReturnSpeed(level int) float64 {
	if level <= 0 {
		return 0.35
	}
	return 0.35 + 0.15*float64(level)
}
