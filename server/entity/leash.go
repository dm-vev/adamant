package entity

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Leashable describes an entity that can be attached to a leash holder.
type Leashable interface {
	AttachLeash(world.Entity, *world.Tx) bool
	DetachLeash(*world.Tx, bool)
}

const leashHolderKey = "LeasherID"

// AttachLeash attaches an Ent to a leash holder and persists the holder's BDS unique ID.
func (e *Ent) AttachLeash(holder world.Entity, tx *world.Tx) bool {
	if holder == nil || e.H() == nil || e.HolderLeashID() != 0 {
		return false
	}
	e.setLeashHolder(holder.H().UniqueID())
	e.refreshLeashMetadata(tx)
	return true
}

// HolderLeashID returns the persisted leash holder ID, or zero if not leashed.
func (e *Ent) HolderLeashID() int64 {
	if e.data.NativeNBT == nil {
		return 0
	}
	id, _ := e.data.NativeNBT[leashHolderKey].(int64)
	return id
}

func (e *Ent) setLeashHolder(id int64) {
	if e.data.NativeNBT == nil {
		e.data.NativeNBT = map[string]any{}
	}
	if id == 0 {
		delete(e.data.NativeNBT, leashHolderKey)
		return
	}
	e.data.NativeNBT[leashHolderKey] = id
}

func (e *Ent) refreshLeashMetadata(tx *world.Tx) {
	if tx == nil {
		return
	}
	viewers := tx.Viewers(e.Position())
	for _, viewer := range viewers {
		viewer.ViewEntityState(e)
	}
	tx.ReleaseViewers(viewers)
}

// DetachLeash removes the leash, optionally dropping its lead.
func (e *Ent) DetachLeash(tx *world.Tx, drop bool) {
	if e.HolderLeashID() == 0 {
		return
	}
	e.setLeashHolder(0)
	e.refreshLeashMetadata(tx)
	if drop && tx != nil {
		tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: e.Position()}, item.NewStack(item.Lead{}, 1)))
	}
}

func tickLeash(e *Ent, tx *world.Tx, movement *Movement) {
	id := e.HolderLeashID()
	if id == 0 {
		return
	}
	var holder world.Entity
	for candidate := range tx.Entities() {
		if candidate.H() != nil && candidate.H().UniqueID() == id {
			holder = candidate
			break
		}
	}
	if holder == nil {
		e.DetachLeash(tx, true)
		return
	}
	delta := holder.Position().Sub(e.Position())
	distance := delta.Len()
	if distance > 10 {
		e.DetachLeash(tx, true)
		return
	}
	if distance > 6 && distance > 0 {
		oldVelocity := e.Velocity()
		newVelocity := oldVelocity.Add(delta.Mul(.4 / distance))
		e.SetVelocity(newVelocity)
		if movement != nil {
			movement.dvel = movement.dvel.Add(newVelocity.Sub(movement.vel))
			movement.vel = newVelocity
		}
	}
}

// TieLeashedEntities creates a fence knot when the user has nearby leashable mobs.
func TieLeashedEntities(pos cube.Pos, tx *world.Tx, user world.Entity) bool {
	switch tx.Block(pos).(type) {
	case block.WoodFence, block.NetherBrickFence:
	default:
		return false
	}
	if user == nil || user.H() == nil {
		return false
	}
	var mobs []*Ent
	for candidate := range tx.EntitiesWithin(cube.Box(-7, -7, -7, 7, 7, 7).Translate(pos.Vec3())) {
		if e, ok := candidate.(interface{ LeashEnt() *Ent }); ok && e.LeashEnt().HolderLeashID() == user.H().UniqueID() {
			mobs = append(mobs, e.LeashEnt())
		}
	}
	if len(mobs) == 0 {
		return false
	}
	opts := world.EntitySpawnOpts{Position: pos.Vec3().Add(mgl64.Vec3{.5, .375, .5})}
	handle := NewLeashKnot(opts, pos)
	tx.AddEntity(handle)
	for _, e := range mobs {
		e.setLeashHolder(handle.UniqueID())
		e.refreshLeashMetadata(tx)
	}
	return true
}

// LeashEnt exposes the shared entity base to leash interaction code.
func (e *Ent) LeashEnt() *Ent { return e }
