package enchantment

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

var Smite smite
var BaneOfArthropods baneOfArthropods

// Smite and Bane of Arthropods go on the items of #enchantable/weapon, a mace
// among them, and an enchanting table offers them for melee weapons only
// (data/minecraft/enchantment/smite.json and bane_of_arthropods.json).

type smite struct{}
type baneOfArthropods struct{}

func (smite) Name() string                   { return "Smite" }
func (smite) MaxLevel() int                  { return 5 }
func (smite) Cost(level int) (int, int)      { n := 5 + (level-1)*8; return n, n + 20 }
func (smite) Rarity() item.EnchantmentRarity { return item.EnchantmentRarityUncommon }
func (smite) CompatibleWithEnchantment(t item.EnchantmentType) bool {
	return damageCompatible(Smite, t)
}
func (smite) CompatibleWithItem(i world.Item) bool { return weapon(i) }
func (smite) PrimaryItem(i world.Item) bool        { return meleeWeapon(i) }
func (smite) Addend(level int) float64             { return float64(level) * 2.5 }

func (baneOfArthropods) Name() string                   { return "Bane of Arthropods" }
func (baneOfArthropods) MaxLevel() int                  { return 5 }
func (baneOfArthropods) Cost(level int) (int, int)      { n := 5 + (level-1)*8; return n, n + 20 }
func (baneOfArthropods) Rarity() item.EnchantmentRarity { return item.EnchantmentRarityUncommon }
func (baneOfArthropods) CompatibleWithEnchantment(t item.EnchantmentType) bool {
	return damageCompatible(BaneOfArthropods, t)
}
func (baneOfArthropods) CompatibleWithItem(i world.Item) bool { return weapon(i) }
func (baneOfArthropods) PrimaryItem(i world.Item) bool        { return meleeWeapon(i) }
func (baneOfArthropods) Addend(level int) float64             { return float64(level) * 2.5 }

// damageCompatible reports whether t may join self on an item. The
// enchantments of Java's #exclusive_set/damage (sharpness, smite, bane of
// arthropods, impaling, density and breach) exclude each other.
func damageCompatible(self, t item.EnchantmentType) bool {
	if t == self {
		return true
	}
	switch t.(type) {
	case sharpness, smite, baneOfArthropods, impaling, density, breach:
		return false
	}
	return true
}
