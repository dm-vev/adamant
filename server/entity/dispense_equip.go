package entity

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// equipWithDispenser is the EquipWithDispenser of the default registry. It
// equips the item.Stack passed onto an entity that wears it from a dispenser: an
// armour stand takes armour in a free slot, a tamed horse a saddle, horse armour
// or a carpet, and a donkey, mule or llama a chest. Other entities take nothing.
func equipWithDispenser(tx *world.Tx, e world.Entity, v any) bool {
	stack, ok := v.(item.Stack)
	if !ok {
		return false
	}
	switch e := e.(type) {
	case interface {
		dispenserEquip(*world.Tx, item.Stack) bool
	}:
		return e.dispenserEquip(tx, stack)
	case *Ent:
		if b, ok := e.Behaviour().(*ArmourStandBehaviour); ok {
			return b.dispenserEquip(e, tx, stack)
		}
	}
	return false
}

// dispenserEquip ports LivingEntity.canEquipWithDispenser and setItemSlot for an
// armour stand: it wears a piece of armour, a pumpkin or a skull in the slot of
// the piece if that slot is free.
func (a *ArmourStandBehaviour) dispenserEquip(e *Ent, tx *world.Tx, stack item.Stack) bool {
	inv := a.armours
	switch i := stack.Item().(type) {
	case item.HelmetType:
		if !i.Helmet() || !inv.Helmet().Empty() {
			return false
		}
		inv.SetHelmet(stack)
	case item.ChestplateType:
		if !i.Chestplate() || !inv.Chestplate().Empty() {
			return false
		}
		inv.SetChestplate(stack)
	case item.LeggingsType:
		if !i.Leggings() || !inv.Leggings().Empty() {
			return false
		}
		inv.SetLeggings(stack)
	case item.BootsType:
		if !i.Boots() || !inv.Boots().Empty() {
			return false
		}
		inv.SetBoots(stack)
	default:
		return false
	}
	a.updateArmours(e)
	tx.PlaySound(e.Position(), sound.EquipItem{Item: stack.Item()})
	return true
}
