package block

import (
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/item/enchantment"
	"github.com/dm-vev/adamant/server/item/inventory"
	"github.com/dm-vev/adamant/server/world"
)

// FrostWalkerLevel returns the level of Frost Walker on the boots e wears.
// An entity wearing it does not burn from stepping on magma blocks or
// campfires, the burn_from_stepping damage types.
func FrostWalkerLevel(e world.Entity) int {
	var boots item.Stack
	switch a := e.(type) {
	case interface{ Armour() *inventory.Armour }:
		boots = a.Armour().Boots()
	case interface {
		EquipmentArmour() (item.Stack, item.Stack, item.Stack, item.Stack)
	}:
		_, _, _, boots = a.EquipmentArmour()
	}
	if ench, ok := boots.Enchantment(enchantment.FrostWalker); ok {
		return ench.Level()
	}
	return 0
}
