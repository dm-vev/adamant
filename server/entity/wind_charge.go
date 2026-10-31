package entity

import (
	"math"

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
	windChargeRadius         = 4.0
	windChargeBaseForce      = 0.65
	windChargeBaseHeight     = 0.6
	windChargeBaseDamage     = 1
	selfBoostHorizontalLimit = 0.75
	selfBoostMinHeight       = 1.0
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
	Drag:    0,
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

// TickAfterBlocks keeps native burst callbacks after the bell actor's pass.
func (windChargeType) TickAfterBlocks() bool { return true }

func (windChargeType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.15625, 0, -0.15625, 0.15625, 0.3125, 0.15625)
}

func (windChargeType) DecodeNBT(_ map[string]any, data *world.EntityData) {
	data.Data = windChargeConf.New()
}

func (windChargeType) EncodeNBT(*world.EntityData) map[string]any { return nil }

func windChargeHit(e *Ent, tx *world.Tx, target trace.Result) {
	origin := windChargeBurstOrigin(target)
	tx.AddParticle(origin, particle.WindBurst{})
	tx.PlaySound(origin, sound.WindChargeImpact{})

	behaviour, _ := e.Behaviour().(*ProjectileBehaviour)
	var owner world.Entity
	if behaviour != nil {
		if handle := behaviour.Owner(); handle != nil {
			owner, _ = handle.Entity(tx)
		}
	}

	windChargeInteractBlocks(tx, origin, owner, 1.2)
	// Native impact removes the projectile in this collision pass, not at
	// the start of the following tick. The behaviour's movement is discarded
	// by Ent.Tick once the handle has closed.
	defer e.CloseIn(tx)

	area := cube.Box(
		math.Floor(origin[0]-windChargeRadius),
		math.Floor(origin[1]-windChargeRadius),
		math.Floor(origin[2]-windChargeRadius),
		math.Ceil(origin[0]+windChargeRadius),
		math.Ceil(origin[1]+windChargeRadius),
		math.Ceil(origin[2]+windChargeRadius),
	)

	for other := range tx.EntitiesWithin(area.Grow(1)) {
		if other == nil || other.H() == e.H() {
			continue
		}

		pos := other.Position()
		dist := pos.Sub(origin).Len()
		if dist > windChargeRadius {
			continue
		}

		if living, ok := other.(Living); ok {
			scale := 1 - dist/windChargeRadius
			if scale <= 0 {
				continue
			}

			force := windChargeBaseForce * scale
			height := windChargeBaseHeight * scale

			knockBackOrigin := origin

			if owner != nil && owner.H() == other.H() {
				horizontalOffset := pos.Sub(origin)
				horizontalOffset[1] = 0
				if horizontalOffset.Len() < selfBoostHorizontalLimit {
					knockBackOrigin = mgl64.Vec3{pos[0], origin[1], pos[2]}
					if height < selfBoostMinHeight {
						height = selfBoostMinHeight
					}
				}
			}

			gustKnockBack(living, knockBackOrigin, force, height)

			if owner == nil || owner.H() != other.H() {
				living.Hurt(windChargeBaseDamage, WindChargeDamageSource{})
			}
			continue
		}
	}
}

func windChargeBurstOrigin(target trace.Result) mgl64.Vec3 {
	origin := target.Position()
	if hit, ok := target.(trace.BlockResult); ok {
		offset := (cube.Pos{}).Side(hit.Face())
		for axis := range origin {
			origin[axis] = float64(float32(origin[axis]) + float32(offset[axis])*.25)
		}
	}
	return origin
}

func windChargeInteractBlocks(tx *world.Tx, origin mgl64.Vec3, owner world.Entity, power float32) {
	positions := windBurstAffectedBlocks(origin, power, func(pos cube.Pos) bool {
		b, loaded := tx.BlockLoaded(pos)
		if !loaded {
			return false
		}
		switch b.(type) {
		case blockpkg.Air, blockpkg.Water:
			return false
		}
		return true
	}, tx.RandomFloat32)

	var user item.User
	if u, ok := owner.(item.User); ok {
		user = u
	}

	for _, pos := range positions {
		blk := tx.Block(pos)
		switch b := blk.(type) {
		case blockpkg.Bell:
			b.ActivateByAttack(pos, tx)
		case blockpkg.Button:
			// ButtonBlock::_onHitByActivatingAttack calls buttonPressed
			// with a null actor, independently of projectile ownership.
			b.Activate(pos, cube.FaceUp, tx, nil, &item.UseContext{})
		case blockpkg.Lever:
			b.Activate(pos, cube.FaceUp, tx, user, &item.UseContext{})
		case blockpkg.WoodDoor:
			if b.Top {
				continue
			}
			ctx := &item.UseContext{}
			b.Activate(pos, cube.FaceUp, tx, user, ctx)
		case blockpkg.CopperDoor:
			if b.Top {
				continue
			}
			ctx := &item.UseContext{}
			b.Activate(pos, cube.FaceUp, tx, user, ctx)
		case blockpkg.WoodTrapdoor:
			ctx := &item.UseContext{}
			b.Activate(pos, cube.FaceUp, tx, user, ctx)
		case blockpkg.CopperTrapdoor:
			ctx := &item.UseContext{}
			b.Activate(pos, cube.FaceUp, tx, user, ctx)
		case blockpkg.WoodFenceGate:
			if user != nil {
				ctx := &item.UseContext{}
				b.Activate(pos, cube.FaceUp, tx, user, ctx)
				continue
			}
			b.Open = !b.Open
			tx.SetBlock(pos, b, nil)
			if b.Open {
				tx.PlaySound(pos.Vec3Centre(), sound.FenceGateOpen{Block: b})
			} else {
				tx.PlaySound(pos.Vec3Centre(), sound.FenceGateClose{Block: b})
			}
		case blockpkg.Candle:
			if !b.Lit {
				continue
			}
			ctx := &item.UseContext{}
			b.Activate(pos, cube.FaceUp, tx, user, ctx)
		case blockpkg.CandleCake:
			if !b.Lit {
				continue
			}
			b.Lit = false
			tx.SetBlock(pos, b, nil)
			tx.PlaySound(pos.Vec3Centre(), sound.FireExtinguish{})
		}
	}
}
