package entity

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// shearable is Java's Shearable: ShearIfReady shears the entity if
// readyForShearing and reports whether it did, with the sound and the drops of
// its shear. A dispenser and a player with shears both shear through it.
type shearable interface {
	ShearIfReady(tx *world.Tx) bool
}

// shearWithDispenser is the ShearWithDispenser of the default registry. It
// ports the body of ShearsDispenseItemBehavior.tryShearEntity for an entity in
// the block at pos: the leads of the entity are cut, or a living entity that is
// ready for shearing is sheared.
func shearWithDispenser(tx *world.Tx, e world.Entity, pos cube.Pos) bool {
	if shearOffLeashes(tx, e, nil) {
		return true
	}
	if s, ok := e.(shearable); ok && s.ShearIfReady(tx) {
		tx.EmitGameEvent(world.GameEventShear, pos.Vec3Centre(), world.GameEventContext{})
		return true
	}
	return false
}

// leashReach is how far from an entity a mob can be that it holds on a lead to:
// the lead snaps at ten blocks.
const leashReach = 12

// shearOffLeashes ports Entity.shearOffAllLeashConnections: the lead of the
// entity, and those of the mobs it holds, drop, and shears snip. The shear game
// event comes from the player passed, which is nil for a dispenser.
func shearOffLeashes(tx *world.Tx, e world.Entity, by world.Entity) bool {
	dropped := false
	if leashed, ok := e.(interface{ LeashEnt() *Ent }); ok && leashed.LeashEnt().HolderLeashID() != 0 {
		leashed.LeashEnt().DetachLeash(tx, true)
		dropped = true
	}
	id := e.H().UniqueID()
	box := cube.Box(-leashReach, -leashReach, -leashReach, leashReach, leashReach, leashReach).Translate(e.Position())
	for other := range tx.EntitiesWithin(box) {
		if mob, ok := other.(interface{ LeashEnt() *Ent }); ok && mob.LeashEnt().HolderLeashID() == id {
			mob.LeashEnt().DetachLeash(tx, true)
			dropped = true
		}
	}
	if !dropped {
		return false
	}
	tx.EmitGameEvent(world.GameEventShear, e.Position(), world.GameEventContext{Source: by})
	tx.PlaySound(e.Position(), sound.Custom{Name: "item.shears.snip", Volume: 1, Pitch: 1})
	return true
}

// dropShorn spawns what an entity sheds when it is sheared at the position
// given, as spawnAtLocation does: one item to an entity.
func dropShorn(tx *world.Tx, at mgl64.Vec3, shorn item.Stack) {
	for range shorn.Count() {
		tx.AddEntity(NewItemDrop(tx, world.EntitySpawnOpts{Position: at}, shorn.Grow(1-shorn.Count())))
	}
}

// ShearIfReady ports Sheep.readyForShearing and shear: a sheep that is not a
// lamb and has its fleece loses it.
func (s *Sheep) ShearIfReady(tx *world.Tx) bool {
	if s.Baby() {
		return false
	}
	wool := s.Shear(tx)
	if wool.Empty() {
		return false
	}
	tx.PlaySound(s.Position(), sound.Custom{Name: "entity.sheep.shear", Volume: 1, Pitch: 1})
	dropShorn(tx, s.Position().Add(mgl64.Vec3{0, 1}), wool)
	return true
}

// ShearIfReady ports MushroomCow.readyForShearing and shear: a mooshroom that
// is not a calf turns into a cow and sheds its mushrooms.
func (m *Mooshroom) ShearIfReady(tx *world.Tx) bool {
	if m.Baby() || m.Dead() {
		return false
	}
	// The mushrooms drop from the top of the mooshroom, which the cow it
	// turns into replaces.
	at := m.Position().Add(mgl64.Vec3{0, m.H().Type().BBox(m).Height()})
	tx.PlaySound(m.Position(), sound.Custom{Name: "entity.mooshroom.shear", Volume: 1, Pitch: 1})
	dropShorn(tx, at, m.Shear(tx))
	return true
}

// ShearIfReady ports SnowGolem.readyForShearing and shear: a snow golem that
// wears its pumpkin sheds it.
func (m *SnowGolem) ShearIfReady(tx *world.Tx) bool {
	if m.Dead() || !m.HasPumpkin() {
		return false
	}
	m.SetPumpkin(false)
	tx.PlaySound(m.Position(), sound.Custom{Name: "entity.snow_golem.shear", Volume: 1, Pitch: 1})
	dropShorn(tx, EyePosition(m), item.NewStack(block.Pumpkin{Carved: true}, 1))
	return true
}

// ShearIfReady ports CopperGolem.readyForShearing and shear: a copper golem
// with a poppy on its antenna sheds it.
func (c *CopperGolem) ShearIfReady(tx *world.Tx) bool {
	b := c.b()
	if c.Dead() || !inItemTag(b.antenna, "shearable_from_copper_golem") {
		return false
	}
	tx.PlaySound(c.Position(), sound.Custom{Name: "entity.copper_golem.shear", Volume: 1, Pitch: 1})
	antenna := b.antenna
	b.antenna = item.Stack{}
	dropShorn(tx, c.Position().Add(mgl64.Vec3{0, 1.5}), antenna)
	return true
}

// ShearIfReady ports SulfurCube.readyForShearing and shear: a sulfur cube
// that holds a block spits it out.
func (s *SulfurCube) ShearIfReady(tx *world.Tx) bool {
	if s.Dead() || s.b().bodyItem.Empty() {
		return false
	}
	spat := s.Shear()
	tx.PlaySound(s.Position(), sound.Custom{Name: "entity.sulfur_cube.eject", Volume: 1, Pitch: 1})
	dropShorn(tx, s.Position().Add(mgl64.Vec3{0, s.H().Type().BBox(s).Height()}), spat)
	return true
}
