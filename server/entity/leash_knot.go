package entity

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// LeashKnotType is the Bedrock leash-knot actor type.
var LeashKnotType world.EntityType = leashKnotType{}

type leashKnotType struct{}
type leashKnotConfig struct{ Pos cube.Pos }
type leashKnotBehaviour struct{ pos cube.Pos }

func (c leashKnotConfig) Apply(data *world.EntityData) { data.Data = leashKnotBehaviour{pos: c.Pos} }
func (leashKnotType) EncodeEntity() string             { return "minecraft:leash_knot" }
func (leashKnotType) BBox(world.Entity) cube.BBox {
	return cube.Box(-.1875, 0, -.1875, .1875, .5, .1875)
}
func (leashKnotType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		leashKnotConfig{}.Apply(d)
	}
	return &LeashKnot{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (leashKnotType) DecodeNBT(nbt map[string]any, d *world.EntityData) {
	pos := cube.Pos{}
	if p, ok := nbt["TileX"].(int32); ok {
		pos[0] = int(p)
	}
	if p, ok := nbt["TileY"].(int32); ok {
		pos[1] = int(p)
	}
	if p, ok := nbt["TileZ"].(int32); ok {
		pos[2] = int(p)
	}
	d.Data = leashKnotBehaviour{pos: pos}
}
func (leashKnotType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(leashKnotBehaviour)
	return map[string]any{"TileX": int32(b.pos[0]), "TileY": int32(b.pos[1]), "TileZ": int32(b.pos[2])}
}

// LeashKnot represents a lead attachment point on a fence.
type LeashKnot struct{ *Ent }

// NewLeashKnot creates a knot at the supplied fence position.
func NewLeashKnot(opts world.EntitySpawnOpts, pos cube.Pos) *world.EntityHandle {
	opts.Position = pos.Vec3()
	opts.Position[0] += .5
	opts.Position[1] += .375
	opts.Position[2] += .5
	return opts.New(LeashKnotType, leashKnotConfig{Pos: pos})
}

// Tick removes the knot when its supporting fence is gone.
func (k *LeashKnot) Tick(tx *world.Tx, current int64) { k.Ent.Tick(tx, current) }
func (b leashKnotBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if _, ok := tx.Block(b.pos).(interface{ IsFence() }); !ok {
		if _, ok = tx.Block(b.pos).(block.WoodFence); !ok {
			if _, ok = tx.Block(b.pos).(block.NetherBrickFence); !ok {
				releaseKnotLeashes(e, tx)
				_ = e.CloseIn(tx)
				return nil
			}
		}
	}
	return nil
}

func releaseKnotLeashes(knot *Ent, tx *world.Tx) {
	id := knot.H().UniqueID()
	for candidate := range tx.Entities() {
		if mob, ok := candidate.(interface{ LeashEnt() *Ent }); ok && mob.LeashEnt().HolderLeashID() == id {
			mob.LeashEnt().DetachLeash(tx, true)
		}
	}
}

// TransferLeashes attaches the knot's mobs to the interacting entity.
func (k *LeashKnot) TransferLeashes(holder world.Entity, tx *world.Tx) bool {
	if holder == nil || holder.H() == nil {
		return false
	}
	attached := false
	id := k.H().UniqueID()
	for candidate := range tx.Entities() {
		if mob, ok := candidate.(interface{ LeashEnt() *Ent }); ok && mob.LeashEnt().HolderLeashID() == id {
			mob.LeashEnt().setLeashHolder(holder.H().UniqueID())
			mob.LeashEnt().refreshLeashMetadata(tx)
			attached = true
		}
	}

	if attached {
		_ = k.CloseIn(tx)
	}
	return attached
}
