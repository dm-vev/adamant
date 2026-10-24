package entity

import (
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
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
	conf := ProjectileBehaviourConfig{
		Owner: owner, Acceleration: .1, Damage: -1,
		Hit: func(e *Ent, tx *world.Tx, target trace.Result) {
			if hit, ok := target.(trace.EntityResult); ok {
				witherSkullHitEntity(e, tx, owner, hit.Entity())
			}
			block.ExplosionConfig{SuppressUnderwaterImpact: true, NoBlockDamage: mobGriefingDisabled(tx)}.Explode(tx, world.NewEntityExplosionSource(e, 1))
		},
	}
	if charged {
		// A dangerous skull, WitherSkull.getInertia, flies slower.
		conf.Inertia = .73
	}
	return conf
}

// witherSkullHitEntity ports WitherSkull.onHitEntity: the skull of a wither
// hurts for 8, and the wither heals 5 when it kills; a skull without a
// shooter hurts for 5 magic damage. On normal and hard difficulty what it
// hurts withers for 10 and 40 seconds.
func witherSkullHitEntity(e *Ent, tx *world.Tx, owner *world.EntityHandle, target world.Entity) {
	living, ok := target.(Living)
	if !ok {
		return
	}
	var hurt bool
	shooter, _ := owner.Entity(tx)
	if l, ok := shooter.(Living); ok && owner != nil {
		_, hurt = living.Hurt(8, ProjectileDamageSource{Projectile: e, Owner: shooter})
		if hurt && living.Dead() {
			l.Heal(5, nil)
		}
	} else {
		_, hurt = living.Hurt(5, IndirectMagicDamageSource{Projectile: e})
	}
	if !hurt {
		return
	}
	var seconds time.Duration
	switch tx.World().Difficulty() {
	case world.DifficultyNormal:
		seconds = 10
	case world.DifficultyHard:
		seconds = 40
	}
	if seconds > 0 {
		living.AddEffect(effect.New(effect.Wither, 2, seconds*time.Second))
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
