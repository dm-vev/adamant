package block

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Slime is a storage block equivalent to nine slimeballs. It has both sticky and bouncy properties,
// making it useful in conjunction with pistons to move both blocks and entities.
type Slime struct {
	solid
	transparent
}

// EntityLand ports SlimeBlock.fallOn and updateEntityMovementAfterFallOn: a
// sneaking entity takes full fall damage and stays down, others take none
// and bounce back up, non-living entities at 80% speed.
func (Slime) EntityLand(_ cube.Pos, _ *world.Tx, e world.Entity, distance *float64) {
	if s, ok := e.(interface{ Sneaking() bool }); ok && s.Sneaking() {
		return
	}
	*distance = 0
	if v, ok := e.(velocityEntity); ok {
		vel := v.Velocity()
		if vel[1] < 0 {
			vel[1] = -vel[1]
			if _, living := e.(livingEntity); !living {
				vel[1] *= 0.8
			}
			v.SetVelocity(vel)
		}
	}
}

// EntityStepOn ports SlimeBlock.stepOn: an entity walking on slime, not
// sneaking and barely moving up or down, is slowed to 0.4 of its speed.
func (Slime) EntityStepOn(_ cube.Pos, _ *world.Tx, e world.Entity) {
	v, ok := e.(velocityEntity)
	if _, player := e.(interface{ GameMode() world.GameMode }); !ok || player {
		// Players move themselves, slime slowing them on their side.
		return
	}
	if sneaky, ok := e.(interface{ Sneaking() bool }); ok && sneaky.Sneaking() {
		return
	}
	vel := v.Velocity()
	if dy := math.Abs(vel[1]); dy < 0.1 {
		scale := 0.4 + dy*0.2
		v.SetVelocity(mgl64.Vec3{vel[0] * scale, vel[1], vel[2] * scale})
	}
}

// Friction ...
func (Slime) Friction() float64 {
	return 0.8
}

// BreakInfo ...
func (s Slime) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(s))
}

// EncodeItem ...
func (Slime) EncodeItem() (name string, meta int16) {
	return "minecraft:slime", 0
}

// EncodeBlock ...
func (Slime) EncodeBlock() (string, map[string]any) {
	return "minecraft:slime", nil
}
