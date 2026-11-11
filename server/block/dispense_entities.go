package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// equippableByDispenser reports whether a dispenser equips the item onto a
// mob in front, as DispenserBlock.getDefaultDispenseMethod does for an item
// with an Equippable component.
func equippableByDispenser(it world.Item) bool {
	switch it := it.(type) {
	case item.HelmetType:
		if it.Helmet() {
			return true
		}
	case item.ChestplateType:
		if it.Chestplate() {
			return true
		}
	case item.LeggingsType:
		if it.Leggings() {
			return true
		}
	case item.BootsType:
		if it.Boots() {
			return true
		}
	}
	switch it.(type) {
	case item.Saddle, Carpet, item.Shield, item.WolfArmor, item.NautilusArmor,
		item.LeatherHorseArmour, item.CopperHorseArmour, item.IronHorseArmour,
		item.GoldenHorseArmour, item.DiamondHorseArmour, item.NetheriteHorseArmour:
		return true
	}
	return false
}

// equipWithDispenser equips one item of the stack onto the entity if it can wear
// it, as LivingEntity.canEquipWithDispenser and setItemSlot do: it must be
// alive and not a spectator, and wear the item in a slot that is free. Players
// take armour; armour stands and mounts take theirs through the registry's
// EquipWithDispenser. A mob takes nothing, as Mob.canDispenserEquipIntoSlot asks
// it to pick up loot.
func equipWithDispenser(tx *world.Tx, e world.Entity, one item.Stack) bool {
	if dead, ok := e.(interface{ Dead() bool }); ok && dead.Dead() {
		return false
	}
	if g, ok := e.(interface{ GameMode() world.GameMode }); ok && g.GameMode() == world.GameModeSpectator {
		return false
	}
	if equip := tx.World().EntityRegistry().Config().EquipWithDispenser; equip != nil && equip(tx, e, one) {
		return true
	}
	if _, shield := one.Item().(item.Shield); shield {
		return equipOffHand(tx, e, one)
	}
	wearer, ok := e.(interface{ Armour() *inventory.Armour })
	if !ok {
		return false
	}
	inv := wearer.Armour().Inventory()
	for slot := range inv.Size() {
		if cur, _ := inv.Item(slot); cur.Empty() && inv.Accepts(slot, one) {
			if inv.SetItem(slot, one) != nil {
				return false
			}
			tx.PlaySound(e.Position(), sound.EquipItem{Item: one.Item()})
			return true
		}
	}
	return false
}

// equipOffHand puts the item in the off hand of the entity if it holds items in
// its hands and the off hand is empty: a shield is equippable in that slot.
func equipOffHand(tx *world.Tx, e world.Entity, one item.Stack) bool {
	hands, ok := e.(interface {
		HeldItems() (mainHand, offHand item.Stack)
		SetHeldItems(mainHand, offHand item.Stack)
	})
	if !ok {
		return false
	}
	mainHand, offHand := hands.HeldItems()
	if !offHand.Empty() {
		return false
	}
	hands.SetHeldItems(mainHand, one)
	tx.PlaySound(e.Position(), sound.EquipItem{Item: one.Item()})
	return true
}

// dispenseEquipment ports EquipmentDispenseItemBehavior.dispenseEquipment: the
// first entity in the block in front that takes an item of the stack wears
// it, and the stack loses one item. It reports whether one did.
func dispenseEquipment(src dispenseSource) bool {
	one := src.stack.Grow(1 - src.stack.Count())
	for e := range entitiesInBlock(src.tx, src.front()) {
		if equipWithDispenser(src.tx, e, one) {
			return true
		}
	}
	return false
}

// dispenseEquip ports EquipmentDispenseItemBehavior: an entity in front wears
// the item, and it is ejected if none does.
func dispenseEquip(src dispenseSource) dispensed {
	if dispenseEquipment(src) {
		return src.done()
	}
	return dispenseDefault(src)
}

// equipped is the result of the optional behaviours that fall back to
// EquipmentDispenseItemBehavior.dispenseEquipment: they keep the item, and the
// dispenser clicks, if no entity wears it.
func equipped(src dispenseSource) dispensed {
	if dispenseEquipment(src) {
		return src.done()
	}
	return src.failed()
}

// dispenseChest ports the behaviour of Items.CHEST: a tamed donkey, mule or
// llama without a chest in front takes it, and otherwise it is ejected.
func dispenseChest(src dispenseSource) dispensed {
	one := src.stack.Grow(1 - src.stack.Count())
	if equip := src.tx.World().EntityRegistry().Config().EquipWithDispenser; equip != nil {
		for e := range entitiesInBlock(src.tx, src.front()) {
			if equip(src.tx, e, one) {
				return src.done()
			}
		}
	}
	return dispenseDefault(src)
}

// dispenseShears ports ShearsDispenseItemBehavior: a beehive or bee nest in
// front with honey is sheared for honeycombs, and otherwise the first entity in
// front whose leads are cut or that is ready for shearing (a sheep, a
// mooshroom, a snow golem, a copper golem, a sulfur cube) is sheared. A success
// wears the shears down by one; with nothing to shear the dispenser keeps
// them. BDS plays no click for shears.
func dispenseShears(src dispenseSource) dispensed {
	tx, front := src.tx, src.front()
	fx := dispenseEffects{click: clickSilent}
	if shearBeehive(tx, front) || shearEntity(tx, front) {
		return dispensed{left: src.stack.Damage(1), effects: fx}
	}
	return dispensed{left: src.stack, unchanged: true, effects: fx}
}

// shearBeehive ports ShearsDispenseItemBehavior.tryShearBeehive: a hive with
// honey level 5 drops its honeycombs and lets its bees out.
func shearBeehive(tx *world.Tx, pos cube.Pos) bool {
	switch hive := tx.Block(pos).(type) {
	case Beehive:
		if hive.HoneyLevel < 5 {
			return false
		}
	case BeeNest:
		if hive.HoneyLevel < 5 {
			return false
		}
	default:
		return false
	}
	// Facing north: the face is only used to turn a pumpkin.
	ctx := &item.UseContext{}
	if !(item.Shears{}).UseOnBlock(pos, cube.FaceNorth, mgl64.Vec3{}, tx, nil, ctx) {
		return false
	}
	if drops := ctx.NewItem; !drops.Empty() {
		dropItem(tx, drops, pos.Vec3Middle().Add(mgl64.Vec3{0, 0.5}))
	}
	tx.EmitGameEvent(world.GameEventShear, pos.Vec3Centre(), world.GameEventContext{})
	return true
}

// shearEntity ports ShearsDispenseItemBehavior.tryShearEntity: the first entity
// in the block, that is not a spectator, whose leads are cut or that is ready
// for shearing, which the entity's own shear decides, is the one the dispenser
// shears.
func shearEntity(tx *world.Tx, pos cube.Pos) bool {
	shear := tx.World().EntityRegistry().Config().ShearWithDispenser
	if shear == nil {
		return false
	}
	for e := range entitiesInBlock(tx, pos) {
		if g, ok := e.(interface{ GameMode() world.GameMode }); ok && g.GameMode() == world.GameModeSpectator {
			continue
		}
		if shear(tx, e, pos) {
			return true
		}
	}
	return false
}

// dispenseBrush ports the behaviour of Items.BRUSH: the first armadillo in front
// that drops a scute for it does, and the brush wears down by 16. Without one
// the dispenser keeps the brush. BDS plays no click for brushes.
func dispenseBrush(src dispenseSource) dispensed {
	fx := dispenseEffects{click: clickSilent}
	for e := range entitiesInBlock(src.tx, src.front()) {
		brushable, ok := e.(interface{ Brush() item.Stack })
		if !ok {
			continue
		}
		if scute := brushable.Brush(); !scute.Empty() {
			dropItem(src.tx, scute, e.Position().Add(mgl64.Vec3{0, 0.5}))
			return dispensed{left: src.stack.Damage(16), effects: fx}
		}
	}
	return dispensed{left: src.stack, unchanged: true, effects: fx}
}
