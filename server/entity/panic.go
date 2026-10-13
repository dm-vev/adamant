package entity

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
)

// panicState ports PanicGoal: damage in the mob's panic causes makes it run
// to random nearby positions, or to water while it burns, for as long as the
// damage is at most 40 ticks old.
type panicState struct {
	source world.DamageSource
	stamp  int64
	active bool
	// environmental limits the panic causes to #panic_environmental_causes,
	// as for wolves, pandas and adult polar bears.
	environmental bool
	// water is the distance at which the mob looks for water to run to even
	// when it does not burn, as turtles do. Zero only looks while burning.
	water int
	// brain makes the panic last 100 to 120 ticks once it starts, as with
	// the AnimalPanic behaviour of mobs with brains.
	brain bool
	until int64
}

// hurt records Java's LivingEntity.lastDamageSource.
func (p *panicState) hurt(src world.DamageSource, tick int64) {
	p.source, p.stamp = src, tick
}

// shouldPanic ports PanicGoal.shouldPanic. The last damage source is
// forgotten 40 ticks after the damage.
func (p *panicState) shouldPanic(tick int64) bool {
	if p.source == nil || tick-p.stamp > 40 {
		return false
	}
	if p.environmental {
		return panicEnvironmentalCause(p.source)
	}
	return panicCause(p.source)
}

// target ports PanicGoal.canUse: the position a panicking mob runs to.
func (p *panicState) target(e *Ent, m randomPosMob) (cube.Pos, bool) {
	distance := p.water
	if e.OnFireDuration() > 0 {
		distance = max(distance, 5)
	}
	if distance > 0 {
		if water, ok := panicWater(m.tx, cube.PosFromVec3(e.Position()), distance); ok {
			return water, true
		}
	}
	return m.randomPos(5, 4)
}

// run runs PanicGoal for a walking mob on nav and reports whether the mob
// panics: it keeps following its path and picks a new target whenever the
// path ends while the panic lasts.
func (p *panicState) run(e *Ent, tx *world.Tx, nav *Navigator) bool {
	tick := tx.CurrentTick()
	if p.brain {
		return p.runBrain(e, tx, nav, tick)
	}
	if p.active {
		if nav.Index < len(nav.Path) {
			return true
		}
		p.active = false
	}
	if !p.shouldPanic(tick) {
		return false
	}
	p.active = p.flee(e, tx, nav)
	return p.active
}

// runBrain ports AnimalPanic: once started by damage the panic lasts 100 to
// 120 ticks, during which the mob runs to a new spot whenever it arrives.
func (p *panicState) runBrain(e *Ent, tx *world.Tx, nav *Navigator, tick int64) bool {
	if tick >= p.until {
		if !p.shouldPanic(tick) {
			p.active = false
			return false
		}
		p.until = tick + 100 + int64(rand.IntN(21))
		nav.Path, nav.Index, nav.HasTarget = nil, 0, false
	}
	p.active = true
	if nav.Index >= len(nav.Path) {
		p.flee(e, tx, nav)
	}
	return true
}

// flee sets a path on nav to the spot the panicking mob runs to.
func (p *panicState) flee(e *Ent, tx *world.Tx, nav *Navigator) bool {
	box := e.H().Type().BBox(e)
	random := java26.NewLegacyRandom(rand.Int64())
	m := randomPosMob{tx: tx, pos: e.data.Pos, home: noHome, profile: GroundProfile{Width: box.Width(), Height: box.Height()}, random: &random}
	goal, ok := p.target(e, m)
	if !ok {
		return false
	}
	path := FindGroundPath(tx, cube.PosFromVec3(e.data.Pos), goal, m.profile)
	if len(path) == 0 {
		return false
	}
	nav.Path, nav.Index, nav.Target, nav.HasTarget, nav.LastComputeTick = path, 0, goal, true, navigatorTick(e)
	return true
}

// panicWater ports PanicGoal.lookForWater: the first water block within a
// Manhattan distance of xz horizontally and one vertically, unless the mob
// stands in a block with collision.
func panicWater(tx *world.Tx, pos cube.Pos, xz int) (cube.Pos, bool) {
	if !isPassable(tx, pos) {
		return cube.Pos{}, false
	}
	for distance := 0; distance <= xz+1; distance++ {
		for dy := -1; dy <= 1; dy++ {
			horizontal := distance - abs(dy)
			if horizontal < 0 || horizontal > xz {
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
