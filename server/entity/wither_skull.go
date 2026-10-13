package entity

import (
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// WitherSkullType is the Wither's native, destructible skull projectile.
var WitherSkullType witherSkullType

type witherSkullType struct{}

func (witherSkullType) EncodeEntity() string        { return "minecraft:wither_skull" }
func (witherSkullType) BBox(world.Entity) cube.BBox { return cube.Box(-.25, 0, -.25, .25, .5, .25) }
func (witherSkullType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		witherSkullConfig(nil, false).Apply(d)
	}
	return &Ent{tx: tx, handle: h, data: d}
}
func (witherSkullType) DecodeNBT(_ map[string]any, d *world.EntityData) {
	witherSkullConfig(nil, false).Apply(d)
}
func (witherSkullType) EncodeNBT(*world.EntityData) map[string]any { return nil }

func witherSkullConfig(owner *world.EntityHandle, charged bool) ProjectileBehaviourConfig {
	return ProjectileBehaviourConfig{
		Owner: owner, Gravity: 0, Drag: .01, Damage: 8,
		Hit: func(e *Ent, tx *world.Tx, target trace.Result) {
			if hit, ok := target.(trace.EntityResult); ok {
				if living, ok := hit.Entity().(Living); ok {
					duration := time.Second * 10
					if charged {
						duration = time.Second * 40
					}
					living.AddEffect(effect.New(effect.Wither, 1, duration))
				}
			}
			block.ExplosionConfig{SuppressUnderwaterImpact: true, NoBlockDamage: mobGriefingDisabled(tx)}.Explode(tx, world.NewEntityExplosionSource(e, 1))
			tx.PlaySound(e.Position(), sound.Custom{Name: "mob.wither.break_block", Volume: 1, Pitch: 1})
		},
	}
}

// NewWitherSkull creates a Wither skull with owner attribution and charge state.
func NewWitherSkull(opts world.EntitySpawnOpts, owner world.Entity, charged bool) *world.EntityHandle {
	var h *world.EntityHandle
	if owner != nil {
		h = owner.H()
	}
	return opts.New(WitherSkullType, witherSkullConfig(h, charged))
}

var _ world.EntityType = witherSkullType{}
