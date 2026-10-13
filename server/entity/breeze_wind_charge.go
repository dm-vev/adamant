package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
)

const breezeWindChargeRadius = 3.0

// BreezeWindChargeType identifies the non-item wind charge fired by a Breeze.
var BreezeWindChargeType breezeWindChargeType

type breezeWindChargeType struct{}

func (breezeWindChargeType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	if data.Data == nil {
		breezeWindChargeConfig(nil).Apply(data)
	}
	return &Ent{tx: tx, handle: handle, data: data}
}
func (breezeWindChargeType) EncodeEntity() string { return "minecraft:breeze_wind_charge" }
func (breezeWindChargeType) BBox(world.Entity) cube.BBox {
	return cube.Box(-.125, -.15, -.125, .125, .1, .125)
}
func (breezeWindChargeType) DecodeNBT(_ map[string]any, data *world.EntityData) {
	breezeWindChargeConfig(nil).Apply(data)
}
func (breezeWindChargeType) EncodeNBT(*world.EntityData) map[string]any { return nil }

func NewBreezeWindCharge(opts world.EntitySpawnOpts, owner world.Entity) *world.EntityHandle {
	var handle *world.EntityHandle
	if owner != nil {
		handle = owner.H()
	}
	return opts.New(BreezeWindChargeType, breezeWindChargeConfig(handle))
}

func breezeWindChargeConfig(owner *world.EntityHandle) ProjectileBehaviourConfig {
	return ProjectileBehaviourConfig{Owner: owner, Gravity: 0, Drag: 0, Damage: 1.4, Hit: breezeWindChargeHit}
}

func breezeWindChargeHit(projectile *Ent, tx *world.Tx, target trace.Result) {
	origin := target.Position()
	tx.AddParticle(origin, particle.WindBurst{})
	tx.PlaySound(origin, sound.BreezeWindChargeBurst{})

	behaviour, _ := projectile.Behaviour().(*ProjectileBehaviour)
	var owner world.Entity
	if behaviour != nil {
		if handle := behaviour.Owner(); handle != nil {
			owner, _ = handle.Entity(tx)
		}
	}
	windChargeInteractBlocks(tx, target, origin, owner)

	windBurstKnockback(tx, origin, breezeWindChargeRadius, projectile.H())
}

var _ world.EntityType = breezeWindChargeType{}
