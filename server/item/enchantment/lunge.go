package enchantment

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Lunge propels the wielder forward after a piercing spear attack.
var Lunge lunge

type lunge struct{}

// Name returns the enchantment name.
func (lunge) Name() string { return "Lunge" }

// MaxLevel returns the maximum Lunge level.
func (lunge) MaxLevel() int { return 3 }

// Cost returns the minimum and maximum enchanting-table cost for the level.
func (lunge) Cost(level int) (int, int) {
	return 5 + (level-1)*8, 25 + (level-1)*8
}

// Rarity returns the enchantment weight.
func (lunge) Rarity() item.EnchantmentRarity { return item.EnchantmentRarityCommon }

// CompatibleWithEnchantment reports whether Lunge can coexist with another enchantment.
func (lunge) CompatibleWithEnchantment(item.EnchantmentType) bool { return true }

// CompatibleWithItem reports whether Lunge applies to the item.
func (lunge) CompatibleWithItem(i world.Item) bool {
	_, ok := i.(item.Spear)
	return ok
}

// Impulse returns the forward velocity added by a successful Lunge effect.
func (lunge) Impulse(level int) float64 {
	if level <= 0 {
		return 0
	}
	return float64(level) * 0.458
}
