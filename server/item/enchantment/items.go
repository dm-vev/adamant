package enchantment

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// The item tags below follow Java's data/minecraft/tags/item/enchantable,
// which the supported_items and primary_items of its enchantments name.

// meleeWeapon reports if i is in #enchantable/melee_weapon: a sword or a
// spear (#swords and #spears).
func meleeWeapon(i world.Item) bool {
	if _, ok := i.(item.Spear); ok {
		return true
	}
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

// armour reports if i is in #enchantable/armor: #head_armor, #chest_armor,
// #leg_armor and #foot_armor. Those hold the helmets, the turtle shell, the
// chestplates, the leggings and the boots, and not the elytra, the skulls,
// the carved pumpkin or the armour of animals, which are worn too.
func armour(i world.Item) bool {
	switch i.(type) {
	case item.Chestplate, item.Leggings, item.Boots:
		return true
	}
	return headArmour(i)
}

// headArmour reports if i is in #enchantable/head_armor: a helmet or the
// turtle shell, not a skull or a carved pumpkin.
func headArmour(i world.Item) bool {
	switch i.(type) {
	case item.Helmet, item.TurtleShell:
		return true
	}
	return false
}

// equippable reports if i is in #enchantable/equippable: armour, the
// elytra, a skull or a carved pumpkin, which a skull and a carved pumpkin
// are here as the head items other than armour.
func equippable(i world.Item) bool {
	if _, ok := i.(item.Elytra); ok || armour(i) {
		return true
	}
	h, ok := i.(item.HelmetType)
	return ok && h.Helmet()
}
