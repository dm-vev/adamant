package enchantment

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// The item tags below follow Java's data/minecraft/tags/item/enchantable,
// which the supported_items and primary_items of its enchantments name.

// meleeWeapon reports if i is in #enchantable/melee_weapon: a sword.
func meleeWeapon(i world.Item) bool {
	t, ok := i.(item.Tool)
	return ok && t.ToolType() == item.TypeSword
}

// sharpWeapon reports if i is in #enchantable/sharp_weapon: a melee weapon
// or an axe.
func sharpWeapon(i world.Item) bool {
	if meleeWeapon(i) {
		return true
	}
	t, ok := i.(item.Tool)
	return ok && t.ToolType() == item.TypeAxe
}

// weapon reports if i is in #enchantable/weapon: a sharp weapon or a mace.
func weapon(i world.Item) bool {
	return sharpWeapon(i) || mace(i)
}

// fireAspectWeapon reports if i is in #enchantable/fire_aspect: a melee
// weapon or a mace.
func fireAspectWeapon(i world.Item) bool {
	return meleeWeapon(i) || mace(i)
}

// mace reports if i is in #enchantable/mace.
func mace(i world.Item) bool {
	_, ok := i.(item.Mace)
	return ok
}
