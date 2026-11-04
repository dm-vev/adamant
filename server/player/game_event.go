package player

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// emitMovementGameEvents follows Entity.applyMovementEmissionAndPlaySound.
// The accumulated float distance and integer next-step boundary deliberately
// retain overshoot; resetting distance at each event changes walking cadence.
func (p *Player) emitMovementGameEvents(delta mgl64.Vec3) {
	if p.GameMode() == world.GameModeSpectator || p.Flying() || p.onGround && p.Sneaking() || p.Riding() != nil {
		return
	}
	supportPos := p.movementOnPos(float32(1e-5))
	support, loaded := p.tx.BlockLoaded(supportPos)
	distance := math.Sqrt(delta[0]*delta[0] + delta[2]*delta[2])
	if !loaded {
		p.moveEventDistance += float32(distance * float64(float32(0.6)))
		if p.nextMoveEventStep == 0 {
			p.nextMoveEventStep = 1
		}
		return
	}
	climbing, _ := p.tx.MovementBlockProperties(support)
	if climbing {
		distance = delta.Len()
	}
	p.moveEventDistance += float32(distance * float64(float32(0.6)))
	if p.nextMoveEventStep == 0 {
		p.nextMoveEventStep = 1
	}
	if p.moveEventDistance <= p.nextMoveEventStep {
		return
	}
	if _, air := support.(block.Air); air {
		return
	}

	effectPos := p.movementOnPos(float32(0.2))
	same := effectPos == supportPos
	effect, loaded := p.tx.BlockLoaded(effectPos)
	if !loaded {
		return
	}
	produced := p.movementStep(effect, delta, same)
	if !same {
		// Both support checks run even when the first one produces effects.
		fromSupport := p.movementStep(support, delta, true)
		produced = produced || fromSupport
	}
	if !produced && p.movementTouchesWater() {
		p.emitGameEvent(world.GameEventSwim, p.Position(), nil)
		produced = true
	}
	if produced {
		p.nextMoveEventStep = float32(int32(p.moveEventDistance) + 1)
	}
}

func (p *Player) movementStep(state world.Block, delta mgl64.Vec3, vibrate bool) bool {
	if _, air := state.(block.Air); air {
		return false
	}
	climbable, _ := p.tx.MovementBlockProperties(state)
	if p.Swimming() || !(p.onGround || climbable || p.Sneaking() && delta[1] == 0) {
		return false
	}
	if vibrate {
		p.emitGameEvent(world.GameEventStep, p.Position(), state)
	}
	return true
}

func (p *Player) movementOnPos(offset float32) cube.Pos {
	if !p.hasMovementSupport {
		return cube.PosFromVec3(p.Position().Sub(mgl64.Vec3{0, float64(offset), 0}))
	}
	pos := p.movementSupport
	if offset > float32(1e-5) {
		b, loaded := p.tx.BlockLoaded(pos)
		if !loaded {
			return pos
		}
		_, high := p.tx.MovementBlockProperties(b)
		if !high {
			pos[1] = int(math.Floor(p.Position()[1] - float64(offset)))
		}
	}
	return pos
}

func (p *Player) movementTouchesWater() bool {
	bounds := Type.BBox(p).Translate(p.Position()).Grow(-0.001)
	low, high := cube.PosFromVec3(bounds.Min()), cube.PosFromVec3(bounds.Max())
	for x := low[0]; x <= high[0]; x++ {
		for y := low[1]; y <= high[1]; y++ {
			for z := low[2]; z <= high[2]; z++ {
				pos := cube.Pos{x, y, z}
				if !p.movementChunkReady(pos) {
					continue
				}
				liquid, ok := p.tx.Liquid(pos)
				if !ok {
					continue
				}
				if _, water := liquid.(block.Water); !water {
					continue
				}
				height := float64(float32(8-liquid.SpreadDecay()) / 9)
				if above, ok := p.tx.Liquid(pos.Side(cube.FaceUp)); ok {
					if _, water := above.(block.Water); water {
						height = 1
					}
				}
				if float64(y)+height >= bounds.Min()[1] {
					return true
				}
			}
		}
	}
	return false
}
