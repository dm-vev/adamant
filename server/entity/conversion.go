package entity

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// conversion ports ConversionParams: how a mob that turns into another
// passes itself on.
type conversion struct {
	// keepEquipment moves the equipment of the mob, with its drop chances,
	// onto the mob it turns into.
	keepEquipment bool
	// preserveCanPickUpLoot passes on whether the mob may pick up loot.
	preserveCanPickUpLoot bool
	// split is ConversionType.SPLIT_ON_DEATH, by which a slime splits: the
	// mob stays, losing its passengers and its lead, rather than being
	// replaced.
	split bool
}

// convertMob ports Mob.convertTo with ConversionType: to, spawned where from
// is, takes over its passengers, vehicle, velocity, equipment and lead,
// effects, name, fire and AI, and from is gone unless it splits.
func convertMob(tx *world.Tx, from world.Entity, to *world.EntityHandle, c conversion) world.Entity {
	if c.split {
		if first, ok := firstPassenger(tx, from); ok {
			mobStopRiding(tx, first)
		}
		if l, ok := from.(Leashable); ok {
			l.DetachLeash(tx, true)
		}
	}
	converted := tx.AddEntity(to)
	if !c.split {
		convertSingle(tx, from, converted, c)
	}
	convertCommon(tx, from, converted, c)
	if !c.split {
		if closer, ok := from.(interface{ CloseIn(*world.Tx) error }); ok {
			_ = closer.CloseIn(tx)
		} else {
			tx.RemoveEntity(from)
		}
	}
	return converted
}

// convertSingle ports ConversionType.SINGLE.convert.
func convertSingle(tx *world.Tx, from, to world.Entity, c conversion) {
	if v, ok := from.(interface{ Velocity() mgl64.Vec3 }); ok {
		if s, ok := to.(interface{ SetVelocity(mgl64.Vec3) }); ok {
			s.SetVelocity(v.Velocity())
		}
	}
	if passenger, ok := firstPassenger(tx, from); ok {
		mobStopRiding(tx, passenger)
		mobStartRiding(tx, passenger, to, true)
	}
	if r, ok := from.(Rider); ok && r.Riding() != nil {
		if vehicle, ok := r.Riding().Entity(tx); ok {
			mobStopRiding(tx, from)
			mobStartRiding(tx, to, vehicle, true)
		}
	}
	if c.keepEquipment {
		src, okFrom := equipmentHolderOf(from)
		dst, okTo := equipmentHolderOf(to)
		if okFrom && okTo {
			for slot := range equipmentSlots {
				if stack := *src.equipment(slot); !stack.Empty() {
					*dst.equipment(slot) = stack
					dst.setDropChance(slot, src.dropChance(slot))
					*src.equipment(slot) = stack.Grow(-stack.Count())
				}
			}
			viewEquipment(tx, to)
		}
	}
	fromEnt, okFrom := entOf(from)
	toEnt, okTo := entOf(to)
	if okFrom && okTo {
		if holder := fromEnt.HolderLeashID(); holder != 0 {
			fromEnt.setLeashHolder(0)
			toEnt.setLeashHolder(holder)
			toEnt.refreshLeashMetadata(tx)
		}
	}
}

// convertCommon ports ConversionType.convertCommon.
func convertCommon(tx *world.Tx, from, to world.Entity, c conversion) {
	if l, ok := from.(Living); ok {
		if dst, ok := to.(Living); ok {
			for _, e := range l.Effects() {
				dst.AddEffect(e)
			}
		}
	}
	if fm, ok := brainMobOf(from); ok {
		if tm, ok := brainMobOf(to); ok {
			if v, ok := fm.Brain().get(memAngryAt); ok && tm.Brain().check(memAngryAt, memRegistered) {
				tm.Brain().set(memAngryAt, v)
			}
		}
	}
	if c.preserveCanPickUpLoot {
		src, okFrom := lootPickerOf(from)
		dst, okTo := lootPickerOf(to)
		if okFrom && okTo {
			dst.setCanPickUpLoot(src.canPickUpLoot())
		}
	}
	if fm, ok := mobOf(from); ok {
		if tm, ok := mobOf(to); ok {
			tm.AI.NoAI = fm.AI.NoAI
		}
	}
	fromEnt, okFrom := entOf(from)
	toEnt, okTo := entOf(to)
	if okFrom && okTo {
		if name := fromEnt.NameTag(); name != "" {
			toEnt.SetNameTag(name)
		}
		toEnt.SetAlwaysShowNameTag(fromEnt.AlwaysShowNameTag())
		if fire := fromEnt.OnFireDuration(); fire > 0 {
			toEnt.SetOnFire(fire)
		}
	}
}

// entOf returns the Ent of an entity that is one.
func entOf(e world.Entity) (*Ent, bool) {
	l, ok := e.(interface{ LeashEnt() *Ent })
	if !ok {
		return nil, false
	}
	return l.LeashEnt(), true
}

// equipmentHolderOf returns the equipment of a mob that keeps Java's six
// equipment slots.
func equipmentHolderOf(e world.Entity) (equipmentHolder, bool) {
	ent, ok := entOf(e)
	if !ok {
		return nil, false
	}
	h, ok := ent.Behaviour().(equipmentHolder)
	return h, ok
}

// lootPicker is implemented by the states of mobs that may pick up loot.
type lootPicker interface {
	canPickUpLoot() bool
	setCanPickUpLoot(bool)
}

func lootPickerOf(e world.Entity) (lootPicker, bool) {
	ent, ok := entOf(e)
	if !ok {
		return nil, false
	}
	p, ok := ent.Behaviour().(lootPicker)
	return p, ok
}

func (b *hostileState) canPickUpLoot() bool       { return b.canPickupLoot }
func (b *hostileState) setCanPickUpLoot(v bool)   { b.canPickupLoot = v }
func (b *oceanBehaviour) canPickUpLoot() bool     { return b.canPickupLoot }
func (b *oceanBehaviour) setCanPickUpLoot(v bool) { b.canPickupLoot = v }
