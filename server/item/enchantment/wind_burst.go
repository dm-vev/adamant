package enchantment

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// WindBurst launches the wielder upward after landing a smash attack, enabling chained dives.
var WindBurst windBurst

type windBurst struct{}

// Name ...
func (windBurst) Name() string {
	return "Wind Burst"
}

// MaxLevel ...
func (windBurst) MaxLevel() int {
	return 3
}

// Cost ...
func (windBurst) Cost(level int) (int, int) {
	minCost := 30 + (level-1)*12
	return minCost, minCost + 25
}

// Rarity ...
func (windBurst) Rarity() item.EnchantmentRarity {
	return item.EnchantmentRarityRare
}

// LaunchHeight returns the desired vertical height gain in blocks granted by the enchantment.
func (windBurst) LaunchHeight(level int) float64 {
	if level <= 0 {
		return 0
	}
	return float64(level) * 8
}

// CompatibleWithEnchantment ...
func (windBurst) CompatibleWithEnchantment(item.EnchantmentType) bool {
	return true
}

// CompatibleWithItem ...
func (windBurst) CompatibleWithItem(i world.Item) bool {
	_, ok := i.(item.Mace)
	return ok
}
