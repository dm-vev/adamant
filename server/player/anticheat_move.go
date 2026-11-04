package player

import (
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/go-gl/mathgl/mgl64"
)

// lastMove is what the movement checks keep of the move the client made
// before: its parts, and the state of the player before it.
type lastMove struct {
	horizontal, vertical float64
	ground, liquid       bool
	friction             float64
}

// moveState returns the state the envelopes of a move of delta are made of:
// what the server knows of the player before the move, and of the move
// before it.
func (p *Player) moveState(ac *acState, delta mgl64.Vec3) anticheat.State {
	pos := p.Position()
	return anticheat.State{
		Speed:          p.heldSpeed(ac),
		MaySprint:      !p.sprinting && !p.crawling && (p.hunger.canSprint() || !p.GameMode().AllowsTakingDamage()),
		Ground:         p.onGround,
		Friction:       p.frictionBelow(pos),
		Liquid:         p.inLiquid(),
		Rises:          delta[1] > 1e-6,
		PrevHorizontal: ac.last.horizontal,
		PrevGround:     ac.last.ground,
		PrevFriction:   ac.last.friction,
		PrevLiquid:     ac.last.liquid,
		PrevVertical:   ac.last.vertical,
		Impulse:        p.pendingImpulse(ac),
	}
}

// frictionBelow returns the friction of the block below pos that the
// movement of a player there is slowed by, Java's
// getBlockPosBelowThatAffectsMyMovement: 0.6 but on ice, slime and honey.
func (p *Player) frictionBelow(pos mgl64.Vec3) float64 {
	b, loaded := p.tx.BlockLoaded(cube.PosFromVec3(pos.Sub(mgl64.Vec3{0, 0.5000001, 0})))
	if f, ok := b.(block.Frictional); ok && loaded {
		return f.Friction()
	}
	return anticheat.DefaultFriction
}

// inLiquid reports whether the box of the player touches water or lava.
func (p *Player) inLiquid() bool {
	box := p.H().Type().BBox(p).Translate(p.Position()).Grow(-.001)
	for x := int(math.Floor(box.Min()[0])); x <= int(math.Floor(box.Max()[0])); x++ {
		for y := int(math.Floor(box.Min()[1])); y <= int(math.Floor(box.Max()[1])); y++ {
			for z := int(math.Floor(box.Min()[2])); z <= int(math.Floor(box.Max()[2])); z++ {
				if _, ok := p.tx.Liquid(cube.Pos{x, y, z}); ok {
					return true
				}
			}
		}
	}
	return false
}

// movementChecked reports whether the movement checks judge a move m of the
// player: not in a grace period, a loading screen, a vehicle, a bed or
// death.
func (p *Player) movementChecked(ac *acState, m client.Move) bool {
	if m.Loading || m.Riding || p.inGrace(ac) || p.Dead() || p.immobile || p.sleeping || p.Riding() != nil {
		return false
	}
	return true
}

// checkMove runs the movement checks on a move of delta of the client,
// whose state before it is st, and reports whether the move was refused:
// the client set back, or the player kicked. The first setback stops the
// checks; those that only log go on.
func (p *Player) checkMove(ac *acState, m client.Move, delta mgl64.Vec3, st anticheat.State) (refused bool) {
	if !p.movementChecked(ac, m) {
		return false
	}
	return p.checkSpeed(ac, m, delta, st) || p.checkFly(ac, m, delta)
}

// speedJudged reports whether Speed judges a move m of the player: the
// movement checks do, the player is not exempt, and it neither flies with
// a flight the server granted nor glides.
func (p *Player) speedJudged(ac *acState, m client.Move) bool {
	return p.movementChecked(ac, m) && p.checks(ac, anticheat.Speed) && !p.Flying() && !p.Gliding()
}

// checkSpeed holds the horizontal part of a move to the envelope of Java's
// travel, with the tolerance and the epsilon of Speed.
func (p *Player) checkSpeed(ac *acState, m client.Move, delta mgl64.Vec3, st anticheat.State) (refused bool) {
	if !p.speedJudged(ac, m) {
		return false
	}
	h := math.Hypot(delta[0], delta[2])
	allowed := anticheat.Allowed(anticheat.HorizontalAllowance(st), ac.conf.Speed.Tolerance, ac.conf.Speed.Epsilon)
	if h <= allowed {
		return false
	}
	return p.actOnMove(ac, m, p.violate(ac, anticheat.Speed, "envelope", h-allowed, fmt.Sprintf("%.3f > %.3f", h, allowed)), p.Position())
}

// actOnMove takes the action a movement check returned on a move m:
// a setback puts the client back at pos. It reports whether the move was
// refused.
func (p *Player) actOnMove(ac *acState, m client.Move, act anticheat.Action, pos mgl64.Vec3) bool {
	switch act {
	case anticheat.Setback:
		p.setBack(ac, pos, m.Tick)
		return true
	case anticheat.Kick:
		return true
	}
	return false
}

// recordMove keeps what the movement checks need of a move of delta the
// client made, from the state st before it. If the envelope judged the move
// (judged), the next move carries what physics allowed of it at most, its
// envelope with no tolerance: a move past the envelope that was only logged
// does not lift the envelopes after it, so that the tolerance does not
// compound tick after tick. A move the envelope did not judge, as in flight
// or in a grace period, is carried as it was.
func (ac *acState) recordMove(delta mgl64.Vec3, st anticheat.State, judged bool) {
	h := math.Hypot(delta[0], delta[2])
	if judged {
		h = min(h, anticheat.HorizontalAllowance(st))
	}
	ac.last = lastMove{
		horizontal: h,
		vertical:   delta[1],
		ground:     st.Ground,
		liquid:     st.Liquid,
		friction:   st.Friction,
	}
}

// supportResendTicks is how often the blocks below a hovering player are
// resent at most, as Java's forceSendPlayerSupportBlocks does.
const supportResendTicks = 200

// checkFly holds a move to Java's floating rule, counted per client move: a
// move that does not descend by HoverDescent, of a player with nothing but
// air around and 0.55 below it, floats, and more than HoverTicks such moves
// in a row (eight times as many under Slow Falling) violate Fly. The
// setback puts the player where it last had blocks around it, after the
// blocks below it are resent if ResendSupport, which heals a ghost block.
// A player that may fly, levitates, glides or flies is not judged.
func (p *Player) checkFly(ac *acState, m client.Move, delta mgl64.Vec3) (refused bool) {
	if !p.checks(ac, anticheat.Fly) || p.GameMode().AllowsFlying() || p.Flying() || p.Gliding() ||
		p.heldEffect(ac, &ac.levitation, effect.Levitation) > 0 {
		ac.hover = 0
		return false
	}
	if delta[1] < -ac.conf.Fly.HoverDescent || !p.onlyAirAround(m.Position) {
		ac.hover = 0
		if !p.onlyAirAround(m.Position) {
			ac.support, ac.supported = m.Position, true
		}
		return false
	}
	ac.hover++
	_, slow := p.Effect(effect.SlowFalling)
	limit := anticheat.HoverLimit(ac.conf.Fly.HoverTicks, slow)
	if ac.hover <= limit {
		return false
	}
	act := p.violate(ac, anticheat.Fly, "hover", float64(ac.hover-limit), fmt.Sprintf("%d moves floating > %d", ac.hover, limit))
	if act == anticheat.Setback {
		if ac.conf.Fly.ResendSupport && (ac.supportResent == 0 || p.tickCount-ac.supportResent >= supportResendTicks) {
			ac.supportResent = p.tickCount
			p.resendBlocksBelow(m.Position)
		}
		target := p.Position()
		if ac.supported {
			target = ac.support
		}
		ac.hover = 0
		p.setBack(ac, target, m.Tick)
		return true
	}
	return act == anticheat.Kick
}

// floatBox returns the box Java's noBlocksAround looks in for a player at
// pos: its box grown by 0.0625 and reaching 0.55 lower.
func (p *Player) floatBox(pos mgl64.Vec3) cube.BBox {
	return p.H().Type().BBox(p).Translate(pos).Grow(0.0625).Extend(mgl64.Vec3{0, -0.55, 0})
}

// onlyAirAround reports whether nothing but air is around a player at pos,
// as Java's noBlocksAround judges it: no block and no liquid in floatBox.
// An unloaded block counts as one.
func (p *Player) onlyAirAround(pos mgl64.Vec3) bool {
	box := p.floatBox(pos)
	low, high := cube.PosFromVec3(box.Min()), cube.PosFromVec3(box.Max())
	for x := low[0]; x <= high[0]; x++ {
		for y := low[1]; y <= high[1]; y++ {
			for z := low[2]; z <= high[2]; z++ {
				bp := cube.Pos{x, y, z}
				b, loaded := p.tx.BlockLoaded(bp)
				if !loaded {
					return false
				}
				if _, air := b.(block.Air); !air {
					return false
				}
				if _, liquid := p.tx.Liquid(bp); liquid {
					return false
				}
			}
		}
	}
	return true
}

// resendBlocksBelow resends the blocks below a player at pos, which its
// client may hold for others.
func (p *Player) resendBlocksBelow(pos mgl64.Vec3) {
	box := p.floatBox(pos)
	low, high := cube.PosFromVec3(box.Min()), cube.PosFromVec3(box.Max())
	for x := low[0]; x <= high[0]; x++ {
		for z := low[2]; z <= high[2]; z++ {
			for y := low[1] - 1; y <= low[1]; y++ {
				p.resendNearbyBlock(cube.Pos{x, y, z})
			}
		}
	}
}

// refuseFlightToggle counts a flight the client of the player asked for in
// a game mode without flight as a violation of Fly. The game refuses it
// whatever the action of the check; a change of game mode the client may
// not have seen yet is not counted.
func (p *Player) refuseFlightToggle() {
	ac := p.anticheat()
	if !p.checks(ac, anticheat.Fly) || p.inGrace(ac) {
		return
	}
	p.violate(ac, anticheat.Fly, "toggle", 1, "flight asked for in a game mode without it")
}
