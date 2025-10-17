package enchantment

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"math"
)

// Breach reduces the effectiveness of the target's armour, making smash attacks deadlier against protected enemies.
var Breach breach

type breach struct{}

// Name ...
func (breach) Name() string {
	return "Breach"
}

// MaxLevel ...
func (breach) MaxLevel() int {
	return 4
}

// Cost ...
func (breach) Cost(level int) (int, int) {
	minCost := 25 + (level-1)*10
	return minCost, minCost + 25
}

// Rarity ...
func (breach) Rarity() item.EnchantmentRarity {
	return item.EnchantmentRarityRare
}

// ArmourMultiplier returns the multiplier applied to armour effectiveness.
func (breach) ArmourMultiplier(level int) float64 {
	if level <= 0 {
		return 1
	}
	return math.Max(0, 1-0.15*float64(level))
}

// CompatibleWithEnchantment ...
func (breach) CompatibleWithEnchantment(t item.EnchantmentType) bool {
	return t != Density
}

// CompatibleWithItem ...
func (breach) CompatibleWithItem(i world.Item) bool {
	_, ok := i.(item.Mace)
	return ok
}
