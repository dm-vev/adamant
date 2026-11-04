package entity

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
)

// panicWater ports PanicGoal.lookForWater: the first water block within a
// Manhattan distance of xz horizontally and one vertically, unless the mob
// stands in a block with collision.
func panicWater(tx *world.Tx, pos cube.Pos, xz int) (cube.Pos, bool) {
	if !isPassable(tx, pos) {
		return cube.Pos{}, false
	}
	for distance := 0; distance <= xz+1; distance++ {
		for dy := -1; dy <= 1; dy++ {
			horizontal := distance - absInt(dy)
			if horizontal < 0 || horizontal > xz {
				continue
			}
			for dx := -horizontal; dx <= horizontal; dx++ {
				dz := horizontal - absInt(dx)
				for _, z := range [2]int{dz, -dz} {
					if pathWater(tx, pos.Add(cube.Pos{dx, dy, z})) {
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
	switch src.(type) {
	case AttackDamageSource, SpearDamageSource, MaceSmashDamageSource, ProjectileDamageSource, ExplosionDamageSource,
		WindChargeDamageSource, effect.InstantDamageSource, effect.PoisonDamageSource, effect.WitherDamageSource:
		return true
	}
	return panicEnvironmentalCause(src)
}

// panicEnvironmentalCause reports whether src is in Java's
// #panic_environmental_causes damage type tag.
func panicEnvironmentalCause(src world.DamageSource) bool {
	switch s := src.(type) {
	case LightningDamageSource, FreezeDamageSource, block.FireDamageSource, block.LavaDamageSource, block.MagmaDamageSource:
		return true
	case block.DamageSource:
		_, cactus := s.Block.(block.Cactus)
		return cactus
	}
	return false
}
