package enchantment

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Density is a mace-exclusive enchantment that increases smash attack damage depending on the fall distance.
var Density density

type density struct{}

// Name ...
func (density) Name() string {
	return "Density"
}

// MaxLevel ...
func (density) MaxLevel() int {
	return 5
}

// Cost ...
func (density) Cost(level int) (int, int) {
	minCost := 15 + (level-1)*9
	return minCost, minCost + 20
}

// Rarity ...
func (density) Rarity() item.EnchantmentRarity {
	return item.EnchantmentRarityUncommon
}

// AdditionalDamage returns the extra damage granted per block of fall distance.
func (density) AdditionalDamage(level int, distance float64) float64 {
	if level <= 0 || distance <= 0 {
		return 0
	}
	return float64(level) * 0.5 * distance
}

// CompatibleWithEnchantment ...
func (density) CompatibleWithEnchantment(t item.EnchantmentType) bool {
	return t != Breach
}

// CompatibleWithItem ...
func (density) CompatibleWithItem(i world.Item) bool {
	_, ok := i.(item.Mace)
	return ok
}
