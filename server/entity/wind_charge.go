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
	origin := target.Position()
	tx.AddParticle(origin, particle.WindBurst{})
	tx.PlaySound(origin, sound.WindChargeImpact{})

	behaviour, _ := e.Behaviour().(*ProjectileBehaviour)
	var owner world.Entity
	if behaviour != nil {
		if handle := behaviour.Owner(); handle != nil {
			owner, _ = handle.Entity(tx)
		}
	}

	windChargeInteractBlocks(tx, target, origin, owner)

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

			living.KnockBack(knockBackOrigin, force, height)

			if owner == nil || owner.H() != other.H() {
				living.Hurt(windChargeBaseDamage, WindChargeDamageSource{})
			}
			continue
		}
	}
}

func windChargeInteractBlocks(tx *world.Tx, target trace.Result, origin mgl64.Vec3, owner world.Entity) {
	positions := make(map[cube.Pos]struct{}, 32)
	base := cube.PosFromVec3(origin)
	windChargeGatherNeighbours(positions, base)

	if blockResult, ok := target.(trace.BlockResult); ok {
		windChargeGatherNeighbours(positions, blockResult.BlockPosition())
		windChargeGatherNeighbours(positions, blockResult.BlockPosition().Side(blockResult.Face()))
	}

	var user item.User
	if u, ok := owner.(item.User); ok {
		user = u
	}

	for pos := range positions {
		blk := tx.Block(pos)
		switch b := blk.(type) {
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

func windChargeGatherNeighbours(set map[cube.Pos]struct{}, base cube.Pos) {
	for x := base.X() - 1; x <= base.X()+1; x++ {
		for y := base.Y() - 1; y <= base.Y()+1; y++ {
			for z := base.Z() - 1; z <= base.Z()+1; z++ {
				set[cube.Pos{x, y, z}] = struct{}{}
			}
		}
	}
}
