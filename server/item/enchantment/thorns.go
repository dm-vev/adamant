package enchantment

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Thorns is an enchantment that inflicts damage on attackers.
var Thorns thorns

type thorns struct{}

// Name ...
func (thorns) Name() string {
	return "Thorns"
}

// MaxLevel ...
func (thorns) MaxLevel() int {
	return 3
}

// Cost ...
func (thorns) Cost(level int) (int, int) {
	minCost := 10 + 20*(level-1)
	return minCost, minCost + 50
}

// Rarity ...
func (thorns) Rarity() item.EnchantmentRarity {
	return item.EnchantmentRarityVeryRare
}

// CompatibleWithEnchantment ...
func (thorns) CompatibleWithEnchantment(item.EnchantmentType) bool {
	return true
}

// CompatibleWithItem ...
func (thorns) CompatibleWithItem(i world.Item) bool {
	_, ok := i.(item.Armour)
	return ok
}

// PrimaryItem reports if i is a chestplate: an enchanting table offers
// Thorns for chest armour only (the primary_items of
// data/minecraft/enchantment/thorns.json).
func (thorns) PrimaryItem(i world.Item) bool {
	_, ok := i.(item.Chestplate)
	return ok
}

// ThornsDamageSource is used for damage caused by thorns.
type ThornsDamageSource struct {
	// Owner is the owner of the armour with the thorns enchantment.
	Owner world.Entity
}

func (ThornsDamageSource) ReducedByResistance() bool { return true }
func (ThornsDamageSource) ReducedByArmour() bool     { return false }
func (ThornsDamageSource) Fire() bool                { return false }
func (ThornsDamageSource) IgnoreTotem() bool         { return false }
