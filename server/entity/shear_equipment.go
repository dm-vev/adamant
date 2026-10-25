package entity

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports Mob.attemptToShearEquipment for the mounts: shears take
// the shearable equipment off a mob nobody rides, its body armour or carpet
// before its saddle, as EquipmentSlot.VALUES orders them.

// shearOff ports Mob.shearItem: the item drops at the seat of the mob, which
// plays its shearing sound and emits a shear game event.
func shearOff(tx *world.Tx, e world.Entity, user world.Entity, stack item.Stack, seatHeight float64, soundType string) {
	pos := e.Position().Add(mgl64.Vec3{0, seatHeight, 0})
	tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: pos}, stack))
	tx.PlaySound(e.Position(), sound.NativeEvent{SoundType: soundType, EntityType: e.H().Type().EncodeEntity(), ExtraData: -1})
	tx.EmitGameEvent(world.GameEventShear, e.Position(), world.GameEventContext{Source: user})
}

// ShearEquipment shears the carpet or horse armour, and otherwise the saddle,
// off a mount nobody rides.
func (m *Mount) ShearEquipment(tx *world.Tx, user world.Entity) bool {
	b := m.b()
	if b.vehicle() {
		return false
	}
	seat := m.H().Type().BBox(m).Height() * .75
	if !b.armor.Empty() {
		armor := b.armor
		b.armor = item.Stack{}
		m.viewArmour(tx)
		m.updateState()
		shearOff(tx, m, user, armor, seat, "armor.unequip_generic")
		return true
	}
	if b.saddled {
		b.saddled = false
		m.updateState()
		shearOff(tx, m, user, item.NewStack(item.Saddle{}, 1), seat, "unsaddle")
		return true
	}
	return false
}

// ShearEquipment shears the saddle off a pig nobody rides.
func (p *Pig) ShearEquipment(tx *world.Tx, user world.Entity) bool {
	b := p.behaviour()
	if !b.saddled || b.passenger != nil {
		return false
	}
	b.saddled = false
	p.updateState()
	shearOff(tx, p, user, item.NewStack(item.Saddle{}, 1), .86875, "unsaddle")
	return true
}

// ShearEquipment shears the saddle off a strider nobody rides.
func (s *Strider) ShearEquipment(tx *world.Tx, user world.Entity) bool {
	b := s.b()
	if !b.saddled || b.passenger != nil {
		return false
	}
	b.saddled = false
	s.updateState()
	shearOff(tx, s, user, item.NewStack(item.Saddle{}, 1), 1.6, "unsaddle")
	return true
}
