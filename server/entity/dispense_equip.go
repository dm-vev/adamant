package entity

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// equipWithDispenser is the EquipWithDispenser of the default registry. It
// equips the item.Stack passed onto an entity that wears it from a dispenser: an
// armour stand takes armour in a free slot, a tamed horse a saddle, horse armour
// or a carpet, and a donkey, mule or llama a chest. A mob that keeps six
// equipment slots takes the rest if it may pick up loot; other entities take
// nothing.
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
	return dispenserEquipGear(tx, e, stack)
}

// dispenserWearer is implemented by the states of mobs that keep Java's six
// equipment slots and know whether they may pick up loot.
type dispenserWearer interface {
	equipmentHolder
	canPickUpLoot() bool
}

// dispenserSlot ports LivingEntity.getEquipmentSlotForItem for the items a
// dispenser equips: the Equippable component of armour, a skull, a carved
// pumpkin, an elytra and a shield, which is for the off hand.
func dispenserSlot(stack item.Stack) (int, bool) {
	switch it := stack.Item().(type) {
	case item.Shield:
		return slotOffhand, true
	case item.HelmetType:
		return slotHead, it.Helmet()
	case item.ChestplateType:
		return slotChest, it.Chestplate()
	case item.LeggingsType:
		return slotLegs, it.Leggings()
	case item.BootsType:
		return slotFeet, it.Boots()
	}
	return 0, false
}

// dispenserEquipGear ports LivingEntity.canEquipWithDispenser,
// Mob.canDispenserEquipIntoSlot and EquipmentDispenseItemBehavior.dispenseEquipment
// for a mob that keeps six equipment slots: it takes an Equippable item into
// its empty slot if it may pick up loot, and the item is then sure to drop and
// the mob stays in the world. A mob whose hands keep only a main-hand item has
// no slot for a shield and takes nothing.
func dispenserEquipGear(tx *world.Tx, e world.Entity, stack item.Stack) bool {
	if dead, ok := e.(interface{ Dead() bool }); ok && dead.Dead() {
		return false
	}
	ent, ok := entOf(e)
	if !ok {
		return false
	}
	wearer, ok := ent.Behaviour().(dispenserWearer)
	if !ok || !wearer.canPickUpLoot() {
		return false
	}
	slot, ok := dispenserSlot(stack)
	if !ok || !wearer.equipment(slot).Empty() {
		return false
	}
	*wearer.equipment(slot) = stack
	wearer.setDropChance(slot, 2)
	ent.SetPersistenceRequired(true)
	viewEquipment(tx, e)
	tx.PlaySound(e.Position(), sound.EquipItem{Item: stack.Item()})
	return true
}

// dispenserEquip ports LivingEntity.canEquipWithDispenser and setItemSlot for an
// armour stand: it wears a piece of armour, a pumpkin or a skull in the slot of
// the piece if that slot is free, and holds a shield in its off hand.
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
	case item.Shield:
		// A shield is equippable in the off hand.
		mainHand, offHand := a.HeldItems()
		if !offHand.Empty() {
			return false
		}
		a.SetHeldItems(e, mainHand, stack)
		tx.PlaySound(e.Position(), sound.EquipItem{Item: stack.Item()})
		return true
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
func (p *Pig) dispenserEquip(tx *world.Tx, stack item.Stack) bool {
	if _, saddle := stack.Item().(item.Saddle); !saddle || !p.Saddle() {
		return false
	}
	playSaddleEquip(tx, p, "entity.pig.saddle")
	return true
}

// dispenserEquip ports LivingEntity.canEquipWithDispenser and
// Strider.canDispenserEquipIntoSlot: an adult strider takes a saddle, which it
// does not wear yet.
func (s *Strider) dispenserEquip(tx *world.Tx, stack item.Stack) bool {
	b := s.b()
	if _, saddle := stack.Item().(item.Saddle); !saddle || b.saddled || b.age < 0 || s.Dead() {
		return false
	}
	b.saddled = true
	playSaddleEquip(tx, s, "entity.strider.saddle")
	return true
}

// dispenserEquip ports LivingEntity.canEquipWithDispenser and
// AbstractNautilus.canDispenserEquipIntoSlot: a tamed adult nautilus takes a
// saddle and nautilus armour in the slots that are free. The other ocean mobs
// take armour like any mob that picks up loot.
func (m *oceanMob) dispenserEquip(tx *world.Tx, stack item.Stack) bool {
	b := m.b()
	if b.kind != oceanNautilus && b.kind != oceanZombieNautilus {
		// A drowned wears what it may pick up like a zombie, and a guardian
		// the same if it were allowed to.
		return dispenserEquipGear(tx, m, stack)
	}
	if !b.tamed || b.age < 0 || m.Dead() {
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

// playSaddleEquip plays the sound a mob makes when it is saddled, as
// LivingEntity.onEquipItem does with the getEquipSound of the mob: Pig and
// Strider override the horse saddle sound of the Equippable of a saddle with
// their own.
func playSaddleEquip(tx *world.Tx, e world.Entity, name string) {
	tx.PlaySound(e.Position(), sound.Custom{Name: name, Volume: 1, Pitch: 1})
}
