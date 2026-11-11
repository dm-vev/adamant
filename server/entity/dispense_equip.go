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

// dispenserEquip ports LivingEntity.canEquipWithDispenser and
// Pig.canDispenserEquipIntoSlot: an adult pig takes a saddle, which it does not
// wear yet, whether tame or not.
func (p *Pig) dispenserEquip(_ *world.Tx, stack item.Stack) bool {
	_, saddle := stack.Item().(item.Saddle)
	return saddle && p.Saddle()
}

// dispenserEquip ports LivingEntity.canEquipWithDispenser and
// Strider.canDispenserEquipIntoSlot: an adult strider takes a saddle, which it
// does not wear yet.
func (s *Strider) dispenserEquip(_ *world.Tx, stack item.Stack) bool {
	b := s.b()
	if _, saddle := stack.Item().(item.Saddle); !saddle || b.saddled || b.age < 0 || s.Dead() {
		return false
	}
	b.saddled = true
	return true
}

// dispenserEquip ports LivingEntity.canEquipWithDispenser and
// AbstractNautilus.canDispenserEquipIntoSlot: a tamed adult nautilus takes a
// saddle and nautilus armour in the slots that are free.
func (m *oceanMob) dispenserEquip(tx *world.Tx, stack item.Stack) bool {
	b := m.b()
	if (b.kind != oceanNautilus && b.kind != oceanZombieNautilus) || !b.tamed || b.age < 0 || m.Dead() {
		return false
	}
	switch it := stack.Item().(type) {
	case item.Saddle:
		if b.saddle {
			return false
		}
		b.saddle = true
		tx.PlaySound(m.Position(), sound.Custom{Name: "item.nautilus_saddle_equip", Volume: 1, Pitch: 1})
		return true
	case item.NautilusArmor:
		if b.bodyArmorMaterial >= 0 {
			return false
		}
		b.bodyArmorMaterial = int(it.Material)
		tx.PlaySound(m.Position(), sound.Custom{Name: "item.armor.equip_nautilus", Volume: 1, Pitch: 1})
		return true
	}
	return false
}
