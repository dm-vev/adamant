package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
)

// PowderSnow is a block of loose snow that entities sink into and freeze in.
// Empty buckets pick it up.
type PowderSnow struct {
	transparent
	empty
}

// BreakInfo ...
func (PowderSnow) BreakInfo() BreakInfo {
	return newBreakInfo(0.25, neverHarvestable, shovelEffective, simpleDrops())
}

// BucketPickup reports that an empty bucket picks the powder snow up.
func (PowderSnow) BucketPickup() bool { return true }

// EntityInside ports PowderSnowBlock.entityInside: entities sink slowly, a
// burning entity melts the snow when it may change the world, and every
// entity inside freezes and is extinguished.
func (s PowderSnow) EntityInside(pos cube.Pos, tx *world.Tx, e world.Entity) {
	_, living := e.(livingEntity)
	if _, feetInSnow := tx.Block(cube.PosFromVec3(e.Position())).(PowderSnow); !living || feetInSnow {
		// makeStuckInBlock: falling restarts, and server simulated movement
		// is spent for this tick. Players move themselves.
		if f, ok := e.(fallDistanceEntity); ok {
			f.ResetFallDistance()
		}
		if _, player := e.(interface{ GameMode() world.GameMode }); !player {
			if v, ok := e.(velocityEntity); ok {
				v.SetVelocity(mgl64.Vec3{})
			}
		}
	}
	flammable, burnable := e.(flammableEntity)
	burning := burnable && flammable.OnFireDuration() > 0
	if burning {
		if _, player := e.(interface{ GameMode() world.GameMode }); player || tx.World().GameRuleBool("mob_griefing") {
			tx.SetBlock(pos, nil, nil)
			tx.AddParticle(pos.Vec3Centre(), particle.BlockBreak{Block: s})
		}
	}
	if freezer, ok := e.(interface{ EnterPowderSnow() }); ok {
		freezer.EnterPowderSnow()
	}
	if burning {
		flammable.Extinguish()
	}
}

// EncodeBlock ...
func (PowderSnow) EncodeBlock() (string, map[string]any) {
	return "minecraft:powder_snow", nil
}
