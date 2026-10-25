package entity

import (
	blockpkg "github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

const (
	// windChargeRadius is WindCharge.RADIUS, and windChargeKnockback the
	// knockback multiplier of its explosion.
	windChargeRadius    = 1.2
	windChargeKnockback = 1.22
)

// NewWindCharge spawns a wind charge projectile owned by the entity passed.
func NewWindCharge(opts world.EntitySpawnOpts, owner world.Entity) *world.EntityHandle {
	conf := windChargeConf
	if owner != nil {
		conf.Owner = owner.H()
	} else {
		conf.Owner = nil
	}
	return opts.New(WindChargeType, conf)
}

var windChargeConf = ProjectileBehaviourConfig{
	Gravity: 0,
	Drag:    0.01,
	Damage:  -1,
	Hit:     windChargeHit,
}

// WindChargeType is a world.EntityType implementation for wind charges.
var WindChargeType windChargeType

type windChargeType struct{}

func (t windChargeType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &Ent{tx: tx, handle: handle, data: data}
}

func (windChargeType) EncodeEntity() string { return "minecraft:wind_charge_projectile" }

func (windChargeType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.125, 0, -0.125, 0.125, 0.25, 0.125)
}

func (windChargeType) DecodeNBT(_ map[string]any, data *world.EntityData) {
	data.Data = windChargeConf.New()
}

func (windChargeType) EncodeNBT(*world.EntityData) map[string]any { return nil }

func windChargeHit(e *Ent, tx *world.Tx, target trace.Result) {
	origin := windChargeImpact(e, tx, target)
	tx.AddParticle(origin, particle.WindBurst{})
	tx.PlaySound(origin, sound.WindChargeImpact{})

	behaviour, _ := e.Behaviour().(*ProjectileBehaviour)
	var owner world.Entity
	if behaviour != nil {
		if handle := behaviour.Owner(); handle != nil {
			owner, _ = handle.Entity(tx)
		}
	}

	windChargeTriggerBlocks(tx, origin, windChargeRadius, owner)
	windGust(tx, e.H(), origin, windChargeRadius, windChargeKnockback, false)
}

// windChargeImpact ports AbstractWindCharge.onHitEntity and onHitBlock: an
// entity hit takes 1 damage and the charge bursts where it is, or a quarter
// of a block off the face of a block hit.
func windChargeImpact(e *Ent, tx *world.Tx, target trace.Result) mgl64.Vec3 {
	switch r := target.(type) {
	case trace.EntityResult:
		var owner world.Entity
		if behaviour, ok := e.Behaviour().(*ProjectileBehaviour); ok {
			owner = behaviour.owner(tx)
		}
		if l, ok := r.Entity().(Living); ok {
			if _, vulnerable := l.Hurt(1, WindChargeDamageSource{Projectile: e, Owner: owner}); vulnerable {
				recordLastHurtMob(owner, l)
			}
		}
		return e.Position()
	case trace.BlockResult:
		return r.Position().Add(cube.Pos{}.Side(r.Face()).Vec3().Mul(.25))
	}
	return target.Position()
}

// windChargeTriggerBlocks ports the TRIGGER block interaction of the
// explosion of a wind charge: the blocks its rays reach react as their
// onExplosionHit does when Explosion.canTriggerBlocks.
func windChargeTriggerBlocks(tx *world.Tx, origin mgl64.Vec3, radius float64, owner world.Entity) {
	var user item.User
	if u, ok := owner.(item.User); ok {
		user = u
	}
	for _, pos := range blockpkg.TriggerExplosionBlocks(tx, origin, radius) {
		ctx := &item.UseContext{}
		switch b := tx.Block(pos).(type) {
		case blockpkg.WoodDoor:
			if !b.Top {
				b.Activate(pos, cube.FaceUp, tx, user, ctx)
			}
		case blockpkg.CopperDoor:
			if !b.Top {
				b.Activate(pos, cube.FaceUp, tx, user, ctx)
			}
		case blockpkg.WoodTrapdoor:
			b.Activate(pos, cube.FaceUp, tx, user, ctx)
		case blockpkg.CopperTrapdoor:
			b.Activate(pos, cube.FaceUp, tx, user, ctx)
		case blockpkg.WoodFenceGate:
			b.Open = !b.Open
			tx.SetBlock(pos, b, nil)
			if b.Open {
				tx.PlaySound(pos.Vec3Centre(), sound.FenceGateOpen{Block: b})
			} else {
				tx.PlaySound(pos.Vec3Centre(), sound.FenceGateClose{Block: b})
			}
		case blockpkg.Button:
			if !b.Pressed {
				b.Activate(pos, cube.FaceUp, tx, user, ctx)
			}
		case blockpkg.Lever:
			b.Activate(pos, cube.FaceUp, tx, user, ctx)
		case blockpkg.Bell:
			b.RingFacing(pos, tx)
		case blockpkg.Candle:
			if b.Lit {
				b.Activate(pos, cube.FaceUp, tx, user, ctx)
			}
		case blockpkg.CandleCake:
			if b.Lit {
				b.Lit = false
				tx.SetBlock(pos, b, nil)
				tx.PlaySound(pos.Vec3Centre(), sound.FireExtinguish{})
			}
		}
	}
}
