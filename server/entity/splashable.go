package entity

import (
	"math"

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
			// ThrownSplashPotion.onHitAsPotion: entities within the potion's
			// box grown by 4, 2 and 4 are affected by how far their box,
			// grown by the projectile margin, lies from it.
			hitBox := box
			if res != nil {
				hitBox = box.Translate(res.Position().Sub(pos))
			}
			margin := max(0, min(0.3, (e.Age().Seconds()*20-2)/20))
			// EntitiesWithin goes by position, so the search reaches 4 blocks
			// further for the boxes of large mobs.
			area := hitBox.GrowVec3(mgl64.Vec3{4, 2, 4})
			for otherE := range filterLiving(tx.EntitiesWithin(area.Grow(4))) {
				otherBox := otherE.H().Type().BBox(otherE).Translate(otherE.Position())
				if !otherBox.IntersectsWith(area) {
					continue
				}
				dist := boxDistanceSqr(hitBox, otherBox.Grow(margin))
				if dist >= 16 {
					continue
				}
				f := 1 - math.Sqrt(dist)/4

				splashed := otherE.(Living)
				for _, eff := range effects {
					if _, ok := eff.Type().(effect.LastingType); !ok {
						splashed.AddEffect(effect.NewInstantWithPotency(eff.Type(), eff.Level(), f))
						continue
					}
					// (int)(scale * duration * durationScale + 0.5) ticks,
					// dropped if it would end within 20 ticks.
					ticks := int(f*float64(eff.Duration()/(time.Second/20))*durMul + 0.5)
					if ticks <= 20 {
						continue
					}
					splashed.AddEffect(effect.New(eff.Type().(effect.LastingType), eff.Level(), time.Duration(ticks)*time.Second/20))
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
		if linger && len(effects) > 0 {
			// ThrownLingeringPotion.onHitAsPotion: the cloud forms where the
			// potion was, or at the feet of the entity it hit.
			at := pos
			if r, ok := res.(trace.EntityResult); ok {
				at = r.Entity().Position()
			}
			tx.AddEntity(newLingeringCloud(world.EntitySpawnOpts{Position: at}, pot, e.ProjectileOwner()))
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

// boxDistanceSqr ports AABB.distanceToSqr: the squared distance between two
// boxes, zero where they overlap.
func boxDistanceSqr(a, b cube.BBox) float64 {
	dx := max(a.Min()[0]-b.Max()[0], b.Min()[0]-a.Max()[0], 0)
	dy := max(a.Min()[1]-b.Max()[1], b.Min()[1]-a.Max()[1], 0)
	dz := max(a.Min()[2]-b.Max()[2], b.Min()[2]-a.Max()[2], 0)
	return dx*dx + dy*dy + dz*dz
}
