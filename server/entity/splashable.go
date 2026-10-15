package entity

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"time"
)

// SplashableBlock is a block that can be splashed with a splash bottle.
type SplashableBlock interface {
	world.Block
	// Splash is called when a water bottle splashes onto a block.
	Splash(tx *world.Tx, pos cube.Pos)
}

// waterSensitive is an entity that water hurts, such as an enderman or a
// blaze, as LivingEntity.isSensitiveToWater reports.
type waterSensitive interface {
	IsSensitiveToWater() bool
}

// potionSplash returns a function that creates a potion splash with a specific
// duration multiplier and potion type.
func potionSplash(durMul float64, pot potion.Potion, linger bool) func(e *Ent, tx *world.Tx, res trace.Result) {
	return func(e *Ent, tx *world.Tx, res trace.Result) {
		pos := e.Position()
		effects := pot.Effects()
		box := e.H().Type().BBox(e).Translate(pos)

		if len(effects) > 0 {
			for otherE := range filterLiving(tx.EntitiesWithin(box.GrowVec3(mgl64.Vec3{8.25, 4.25, 8.25}))) {
				otherPos := otherE.Position()
				if !otherE.H().Type().BBox(otherE).Translate(otherPos).IntersectsWith(box.GrowVec3(mgl64.Vec3{4.125, 2.125, 4.125})) {
					continue
				}

				dist := otherPos.Sub(pos).Len()
				if dist > 4 {
					continue
				}

				f := 1 - dist/4
				if entityResult, ok := res.(trace.EntityResult); ok && entityResult.Entity().H() == otherE.H() {
					f = 1
				}

				splashed := otherE.(Living)
				for _, eff := range effects {
					if _, ok := eff.Type().(effect.LastingType); !ok {
						splashed.AddEffect(effect.NewInstantWithPotency(eff.Type(), eff.Level(), f))
						continue
					}

					dur := time.Duration(float64(eff.Duration()) * durMul * f)
					if dur < time.Second {
						continue
					}
					splashed.AddEffect(effect.New(eff.Type().(effect.LastingType), eff.Level(), dur))
				}
			}
		} else if pot == potion.Water() {
			switch result := res.(type) {
			case trace.BlockResult:
				blockPos := result.BlockPosition().Side(result.Face())
				if _, ok := tx.Block(blockPos).(block.Fire); ok {
					tx.SetBlock(blockPos, nil, nil)
				}

				for _, f := range cube.HorizontalFaces() {
					h := blockPos.Side(f)
					if _, ok := tx.Block(h).(block.Fire); ok {
						tx.SetBlock(h, nil, nil)
					}

					if b, ok := tx.Block(blockPos.Side(f)).(SplashableBlock); ok {
						b.Splash(tx, blockPos.Side(f))
					}
				}

				resultPos := result.BlockPosition()
				if b, ok := tx.Block(resultPos).(SplashableBlock); ok {
					b.Splash(tx, resultPos)
				}
			}
			splashWater(e, tx, box)
		}
		if linger {
			tx.AddEntity(NewAreaEffectCloud(world.EntitySpawnOpts{Position: pos}, pot))
		}
	}
}

// splashWater ports AbstractThrownPotion.affectEntitiesAround for water:
// within four blocks, living entities sensitive to water take one point of
// indirect magic damage and burning ones are extinguished, and every
// axolotl in the area is rehydrated.
func splashWater(e *Ent, tx *world.Tx, box cube.BBox) {
	area := box.GrowVec3(mgl64.Vec3{4, 2, 4})
	var owner world.Entity
	if p, ok := e.Behaviour().(*ProjectileBehaviour); ok && p.Owner() != nil {
		owner, _ = p.Owner().Entity(tx)
	}
	for other := range filterLiving(tx.EntitiesWithin(area)) {
		if !other.H().Type().BBox(other).Translate(other.Position()).IntersectsWith(area) {
			continue
		}
		if axolotl, ok := other.(*Axolotl); ok {
			axolotl.Rehydrate()
		}
		if other.Position().Sub(e.Position()).LenSqr() >= 16 {
			continue
		}
		living := other.(Living)
		if sensitive, ok := other.(waterSensitive); ok && sensitive.IsSensitiveToWater() {
			living.Hurt(1, SplashPotionDamageSource{Projectile: e, Owner: owner})
		}
		if burning, ok := other.(interface {
			OnFireDuration() time.Duration
			Extinguish()
		}); ok && burning.OnFireDuration() > 0 && !living.Dead() {
			burning.Extinguish()
		}
	}
}
