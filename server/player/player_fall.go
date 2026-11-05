package player

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
)

// ResetFallDistance resets the player's fall distance.
func (p *Player) ResetFallDistance() {
	p.fallDistance = 0
}

// FallDistance returns the player's fall distance.
func (p *Player) FallDistance() float64 {
	return p.fallDistance
}

// SetFallDistance sets how far the player counts as having fallen, as
// geysers capping it do.
func (p *Player) SetFallDistance(distance float64) {
	p.fallDistance = distance
}

// updateFallState is called to update the entities falling state.
func (p *Player) updateFallState(distanceThisTick float64) {
	switch {
	case p.OnGround():
		p.fallDistance -= distanceThisTick
		if p.fallDistance > 0 {
			low, _ := p.blocksUnder()
			support := cube.Pos{low[0], low[1] - 1, low[2]}
			if state, loaded := p.tx.BlockLoaded(support); loaded {
				p.emitGameEvent(world.GameEventHitGround, p.Position(), state)
			}
		}
		if p.fallDistance > 3 {
			p.fall(p.fallDistance)
		}
		p.ResetFallDistance()
	case distanceThisTick < 0 && distanceThisTick < p.fallDistance:
		p.fallDistance -= distanceThisTick
	default:
		p.ResetFallDistance()
	}
}

// fall is called when a falling entity hits the ground.
func (p *Player) fall(distance float64) {
	if pos, lander, ok := p.landedOn(); ok {
		lander.EntityLand(pos, p.tx, p, &distance)
	}
	p.CauseFallDamage(distance, 1, entity.FallDamageSource{})
}

// CauseFallDamage hurts the player for landing after falling distance blocks,
// the three blocks above the safe fall distance multiplied by multiplier, with
// the damage source passed, or entity.FallDamageSource if nil. Jump Boost
// raises the safe fall distance and the fall_damage game rule disables the
// damage.
func (p *Player) CauseFallDamage(distance, multiplier float64, src world.DamageSource) {
	if !p.tx.World().FallDamage() {
		return
	}
	if src == nil {
		src = entity.FallDamageSource{}
	}
	dmg := distance - 3
	if boost, ok := p.Effect(effect.JumpBoost); ok {
		dmg -= float64(boost.Level())
	}
	if dmg < 0.5 {
		return
	}
	if dmg = math.Ceil(dmg * multiplier); dmg > 0 {
		p.Hurt(dmg, src)
	}
}

// landedOn returns the first block.EntityLander the Player came to rest on, along with its position.
func (p *Player) landedOn() (cube.Pos, block.EntityLander, bool) {
	low, high := p.blocksUnder()
	for x := low[0]; x <= high[0]; x++ {
		for z := low[2]; z <= high[2]; z++ {
			pos := cube.Pos{x, low[1], z}
			if b, loaded := p.tx.BlockLoaded(pos); loaded {
				if lander, ok := b.(block.EntityLander); ok {
					return pos, lander, true
				}
			}
		}
	}
	return cube.Pos{}, nil, false
}

// blocksUnder returns the corners of the range of block positions directly below the Player. Every block in that range
// is one the Player stands on: the Player is narrower than a block, so it may rest on the edge of one with its centre
// over the block beside it, and looking only below its centre would miss the block it is actually standing on.
func (p *Player) blocksUnder() (low, high cube.Pos) {
	box := Type.BBox(p).Translate(p.Position())
	// The Y is taken from the box itself, while the horizontal range is taken from a slightly smaller box so that a
	// Player resting exactly on the boundary between two blocks does not reach into the column beside the one it
	// stands on.
	y := int(math.Floor(box.Min()[1] - 0.0001))
	horizontal := box.Grow(-0.0001)
	low, high = cube.PosFromVec3(horizontal.Min()), cube.PosFromVec3(horizontal.Max())
	low[1], high[1] = y, y
	return low, high
}
