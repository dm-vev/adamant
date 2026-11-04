package enchantment

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Sharpness is an enchantment applied to a sword or axe that increases melee
// damage.
var Sharpness sharpness

type sharpness struct{}

// Name ...
func (sharpness) Name() string {
	return "Sharpness"
}

// MaxLevel ...
func (sharpness) MaxLevel() int {
	return 5
}

// Cost ...
func (sharpness) Cost(level int) (int, int) {
	minCost := 1 + (level-1)*11
	return minCost, minCost + 20
}

// Rarity ...
func (sharpness) Rarity() item.EnchantmentRarity {
	return item.EnchantmentRarityCommon
}

// Addend returns the additional damage when attacking with sharpness.
func (sharpness) Addend(level int) float64 {
	return float64(level) * 1.25
}

// CompatibleWithEnchantment ...
func (sharpness) CompatibleWithEnchantment(t item.EnchantmentType) bool {
	return damageCompatible(Sharpness, t)
}

// CompatibleWithItem reports if i is a sharp weapon, as the
// supported_items of data/minecraft/enchantment/sharpness.json.
func (sharpness) CompatibleWithItem(i world.Item) bool {
	return sharpWeapon(i)
}

// PrimaryItem reports if i is a melee weapon: an enchanting table offers
// Sharpness for a sword but not for an axe.
func (sharpness) PrimaryItem(i world.Item) bool {
	return meleeWeapon(i)
}
