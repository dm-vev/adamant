package entity

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
)

// panicState ports PanicGoal: damage in #panic_causes makes the mob run to
// random nearby positions, or to water while it burns, for as long as the
// damage is at most 40 ticks old.
type panicState struct {
	source world.DamageSource
	stamp  int64
}

// hurt records Java's LivingEntity.lastDamageSource.
func (p *panicState) hurt(src world.DamageSource, tick int64) {
	p.source, p.stamp = src, tick
}

// shouldPanic ports PanicGoal.shouldPanic. The last damage source is
// forgotten 40 ticks after the damage.
func (p *panicState) shouldPanic(tick int64) bool {
	return p.source != nil && tick-p.stamp <= 40 && panicCause(p.source)
}

// target ports PanicGoal.canUse: the position a panicking mob runs to.
func (p *panicState) target(e *Ent, m randomPosMob) (cube.Pos, bool) {
	if e.OnFireDuration() > 0 {
		if water, ok := panicWater(m.tx, cube.PosFromVec3(e.Position())); ok {
			return water, true
		}
	}
	return m.randomPos(5, 4)
}

// panicWater ports PanicGoal.lookForWater: the first water block within a
// Manhattan distance of five horizontally and one vertically, unless the mob
// stands in a block with collision.
func panicWater(tx *world.Tx, pos cube.Pos) (cube.Pos, bool) {
	if !isPassable(tx, pos) {
		return cube.Pos{}, false
	}
	for distance := 0; distance <= 6; distance++ {
		for dy := -1; dy <= 1; dy++ {
			horizontal := distance - abs(dy)
			if horizontal < 0 || horizontal > 5 {
				continue
			}
			for dx := -horizontal; dx <= horizontal; dx++ {
				dz := horizontal - abs(dx)
				for _, z := range [2]int{dz, -dz} {
					if frogWater(tx, pos.Add(cube.Pos{dx, dy, z})) {
						return pos.Add(cube.Pos{dx, dy, z}), true
					}
					if dz == 0 {
						break
					}
				}
			}
		}
	}
	return cube.Pos{}, false
}

// panicCause reports whether src is in Java's #panic_causes damage type tag.
func panicCause(src world.DamageSource) bool {
	switch s := src.(type) {
	case AttackDamageSource, SpearDamageSource, MaceSmashDamageSource, ProjectileDamageSource, ExplosionDamageSource,
		LightningDamageSource, WindChargeDamageSource, FreezeDamageSource,
		block.FireDamageSource, block.LavaDamageSource, block.MagmaDamageSource,
		effect.InstantDamageSource, effect.PoisonDamageSource, effect.WitherDamageSource:
		return true
	case block.DamageSource:
		_, cactus := s.Block.(block.Cactus)
		return cactus
	}
	return false
}
