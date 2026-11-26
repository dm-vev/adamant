package player

import (
	"math"

	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// emitMovementGameEvents follows Entity.applyMovementEmissionAndPlaySound.
// The accumulated float distance and integer next-step boundary deliberately
// retain overshoot; resetting distance at each event changes walking cadence.
func (p *Player) emitMovementGameEvents(delta mgl64.Vec3) {
	if p.GameMode() == world.GameModeSpectator || p.Flying() || p.onGround && p.Sneaking() || p.Riding() != nil {
		return
	}
	distance := math.Sqrt(delta[0]*delta[0] + delta[2]*delta[2])
	nextStep := p.nextMoveEventStep
	if nextStep == 0 {
		nextStep = 1
	}
	if delta[1] == 0 {
		nextDist := p.moveEventDistance + float32(distance*float64(float32(0.6)))
		if nextDist <= nextStep {
			p.moveEventDistance = nextDist
			p.nextMoveEventStep = nextStep
			return
		}
	}
	supportPos := p.movementOnPos(float32(1e-5))
	rid, loaded := p.tx.BlockRID(supportPos)
	if !loaded {
		p.moveEventDistance += float32(distance * float64(float32(0.6)))
		p.nextMoveEventStep = nextStep
		return
	}
	reg := p.tx.World().BlockRegistry()
	flags := reg.BlockFlags(rid)
	if delta[1] != 0 && (flags.IsClimbable() || flags.IsPowderSnow()) {
		distance = delta.Len()
	}
	p.moveEventDistance += float32(distance * float64(float32(0.6)))
	p.nextMoveEventStep = nextStep
	if p.moveEventDistance <= p.nextMoveEventStep || rid == reg.AirRuntimeID() {
		return
	}
	support, ok := p.blockAtRID(supportPos, rid, flags, reg)
	if !ok {
		return
	}
	if _, air := support.(block.Air); air {
		return
	}

	effectPos := p.movementOnPos(float32(0.2))
	same := effectPos == supportPos
	effect := support
	if !same {
		effect, loaded = p.tx.BlockLoaded(effectPos)
		if !loaded {
			return
		}
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
	reg := p.tx.World().BlockRegistry()
	bounds := Type.BBox(p).Translate(p.Position()).Grow(-0.001)
	low, high := cube.PosFromVec3(bounds.Min()), cube.PosFromVec3(bounds.Max())
	for x := low[0]; x <= high[0]; x++ {
		for y := low[1]; y <= high[1]; y++ {
			for z := low[2]; z <= high[2]; z++ {
				pos := cube.Pos{x, y, z}
				rid, ok := p.tx.LiquidRID(pos)
				if !ok {
					continue
				}
				if kind, _, _ := reg.LiquidInfo(rid); kind != 1 {
					continue
				}
				// block.Water.SpreadDecay() is 1.
				height := float64(float32(7) / 9)
				if aboveRID, ok := p.tx.LiquidRID(pos.Side(cube.FaceUp)); ok {
					if aboveKind, _, _ := reg.LiquidInfo(aboveRID); aboveKind == 1 {
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
