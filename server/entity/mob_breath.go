package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item/enchantment"
)

// This file ports what LivingEntity.baseTick does to a mob's breath: a mob
// in a solid block suffocates, and one with its eyes under water runs out of
// air and drowns unless it breathes water.

// maxMobAirSupply is LivingEntity.getMaxAirSupply.
const maxMobAirSupply = 300

// ownAirSupply is implemented by the behaviours of water mobs that keep
// their air supply themselves, as WaterAnimal.handleAirSupply does.
type ownAirSupply interface {
	keepsOwnAirSupply()
}

func (*oceanBehaviour) keepsOwnAirSupply()         {}
func (*oceanWildlifeBehaviour) keepsOwnAirSupply() {}
func (*AquaticMobBehaviour) keepsOwnAirSupply()    {}

// tickBreath ports the breathing half of LivingEntity.baseTick for a mob.
func (ai *MobAI) tickBreath(m *Mob) {
	if !m.Alive() {
		return
	}
	if !ai.NoPhysics && mobInWall(m) {
		if l, ok := m.Self.(Living); ok {
			l.Hurt(1, SuffocationDamageSource{})
		}
		if !m.Alive() {
			return
		}
	}
	if _, own := m.Behaviour().(ownAirSupply); own {
		return
	}
	if !ai.airSet {
		ai.air, ai.airSet = maxMobAirSupply, true
	}
	eye := cube.PosFromVec3(m.data.Pos.Add([3]float64{0, m.EyeY() - m.data.Pos[1], 0}))
	_, bubbles := m.tx.Block(eye).(block.BubbleColumn)
	if eyeInWater(m) && !bubbles {
		if !mobBreathesWater(m) {
			ai.air = ai.decreaseAirSupply(m)
			if ai.air <= -20 {
				ai.air = 0
				if l, ok := m.Self.(Living); ok {
					l.Hurt(2, DrowningDamageSource{})
				}
			}
		} else if ai.air < maxMobAirSupply {
			ai.air = min(ai.air+4, maxMobAirSupply)
		}
		return
	}
	if ai.air < maxMobAirSupply {
		ai.air = min(ai.air+4, maxMobAirSupply)
	}
}

// decreaseAirSupply ports LivingEntity.decreaseAirSupply: Respiration on the
// helmet of a mob now and then saves it a breath.
func (ai *MobAI) decreaseAirSupply(m *Mob) int {
	if m.H().Type() == IronGolemType {
		// IronGolem.decreaseAirSupply: iron golems never run out of air.
		return ai.air
	}
	if h, ok := equipmentHolderOf(m.Self); ok {
		if e, ok := h.equipment(slotHead).Enchantment(enchantment.Respiration); ok && e.Level() > 0 {
			if m.Random().NextDouble() >= 1/float64(e.Level()+1) {
				return ai.air
			}
		}
	}
	return ai.air - 1
}

// mobBreathesWater ports LivingEntity.canBreatheUnderwater and
// MobEffectUtil.hasWaterBreathing.
func mobBreathesWater(m *Mob) bool {
	if entityTypeInTag(m.Self, "can_breathe_under_water") {
		return true
	}
	if c, ok := m.Self.(*SulfurCube); ok && !c.Behaviour().(*SulfurCubeBehaviour).bodyItem.Empty() {
		// SulfurCube.canBreatheUnderwater: a cube holding an item does.
		return true
	}
	if l, ok := m.Self.(interface {
		Effect(effect.Type) (effect.Effect, bool)
	}); ok {
		if _, ok := l.Effect(effect.WaterBreathing); ok {
			return true
		}
		if _, ok := l.Effect(effect.ConduitPower); ok {
			return true
		}
	}
	return false
}

// mobInWall ports Entity.isInWall: a box 0.8 times the width of the mob at
// its eyes overlaps a block that suffocates.
func mobInWall(m *Mob) bool {
	half := m.Width() * .8 / 2
	eyeY := m.EyeY()
	minX, maxX := m.data.Pos[0]-half, m.data.Pos[0]+half
	minZ, maxZ := m.data.Pos[2]-half, m.data.Pos[2]+half
	y := int(math.Floor(eyeY))
	for x := int(math.Floor(minX)); x <= int(math.Floor(maxX)); x++ {
		for z := int(math.Floor(minZ)); z <= int(math.Floor(maxZ)); z++ {
			pos := cube.Pos{x, y, z}
			if shape, ok := blockShape(m.tx, pos); ok && shape.CollisionFull && shape.RedstoneConductor {
				return true
			}
		}
	}
	return false
}
