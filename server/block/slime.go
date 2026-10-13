package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
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
