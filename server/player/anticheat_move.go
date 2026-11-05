package player

import (
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// lastMove is what the movement checks keep of the move the client made
// before: its parts, the vertical part of the one before it, and the state
// of the player before it.
type lastMove struct {
	horizontal, vertical float64
	prevVertical         float64
	ground, liquid       bool
	friction             float64
	// lenient is State.Lenient before the move, and claim is set if the
	// client said it collided below in the move, and rose no more than it
	// steps.
	lenient, claim bool
}

// moveState returns the state the envelopes of a move of delta are made of:
// what the server knows of the player before the move, and of the move
// before it.
func (p *Player) moveState(ac *acState, delta mgl64.Vec3) anticheat.State {
	pos := p.Position()
	ground := p.clientGround(ac)
	return anticheat.State{
		Lenient:        ground && !p.onGround,
		PrevLenient:    ac.last.lenient,
		Speed:          p.heldSpeed(ac),
		MaySprint:      !p.sprinting && !p.crawling && (p.hunger.canSprint() || !p.GameMode().AllowsTakingDamage()),
		Ground:         ground,
		Friction:       p.frictionBelow(pos),
		Liquid:         p.inLiquid(),
		Rises:          delta[1] > 1e-6,
		PrevHorizontal: ac.last.horizontal,
		PrevGround:     ac.last.ground,
		PrevFriction:   ac.last.friction,
		PrevLiquid:     ac.last.liquid,
		PrevVertical:   ac.last.vertical,
		Climbing:       p.climbing(pos),
		Bubbles:        p.upwardBubbles(pos),
		JumpBoost:      p.heldEffect(ac, &ac.jumpBoost, effect.JumpBoost),
		Levitation:     p.heldEffect(ac, &ac.levitation, effect.Levitation),
		JumpFactor:     p.jumpFactorAt(pos),
		SlowFalling:    p.hasEffect(effect.SlowFalling),
		Bounce:         p.bounce(ac, pos, ground),
		Step:           ac.conf.Ascend.StepHeight,
		Impulse:        p.pendingImpulse(ac),
	}
}

// hasEffect reports whether the player has an effect of the type passed.
func (p *Player) hasEffect(t effect.Type) bool {
	_, ok := p.Effect(t)
	return ok
}

// climbing reports whether a player at pos climbs: on a ladder, a vine or
// another climbable block, in scaffolding, or in powder snow in leather
// boots.
func (p *Player) climbing(pos mgl64.Vec3) bool {
	if p.onClimbable() {
		return true
	}
	b, loaded := p.tx.BlockLoaded(cube.PosFromVec3(pos))
	if !loaded {
		return false
	}
	switch b.(type) {
	case block.Scaffolding:
		return true
	case block.PowderSnow:
		boots, ok := p.Armour().Boots().Item().(item.Boots)
		_, leather := boots.Tier.(item.ArmourTierLeather)
		return ok && leather
	}
	return false
}

// upwardBubbles reports whether a player at pos is in or on an upward
// bubble column.
func (p *Player) upwardBubbles(pos mgl64.Vec3) bool {
	feet := cube.PosFromVec3(pos)
	for _, at := range []cube.Pos{feet, feet.Side(cube.FaceDown)} {
		if b, loaded := p.tx.BlockLoaded(at); loaded {
			if c, ok := b.(block.BubbleColumn); ok && !c.DragDown {
				return true
			}
		}
	}
	return false
}

// jumpFactorAt returns what the block a player at pos is in or stands on
// scales its jump by: 0.5 for honey, 1 otherwise.
func (p *Player) jumpFactorAt(pos mgl64.Vec3) float64 {
	for _, at := range []mgl64.Vec3{pos, pos.Sub(mgl64.Vec3{0, 0.5000001, 0})} {
		if b, loaded := p.tx.BlockLoaded(cube.PosFromVec3(at)); loaded {
			if _, honey := b.(block.HoneyBlock); honey {
				return 0.5
			}
		}
	}
	return 1
}

// bounce returns the speed a bouncy block below a player at pos throws it
// up at, from the speed it fell with in the two moves before: slime gives
// it back whole to a player that does not sneak, a bed two thirds of it.
func (p *Player) bounce(ac *acState, pos mgl64.Vec3, ground bool) float64 {
	if !ground || p.sneaking {
		return 0
	}
	b, loaded := p.tx.BlockLoaded(cube.PosFromVec3(pos.Sub(mgl64.Vec3{0, 0.5000001, 0})))
	if !loaded {
		return 0
	}
	restitution := 0.0
	switch b.(type) {
	case block.Slime:
		restitution = 1
	case block.Bed:
		restitution = 2.0 / 3
	default:
		return 0
	}
	fell := max(-ac.last.vertical, -ac.last.prevVertical, 0)
	if fell == 0 {
		return 0
	}
	return (fell + anticheat.Gravity) * restitution
}

// groundAt reports whether a player at pos stands on a block: the box below
// its feet, 0.05 deep, holds the box of a block.
func (p *Player) groundAt(pos mgl64.Vec3) bool {
	return p.groundWithin(pos, 0, 0.05)
}

// groundWithin reports whether the box below a player at pos holds the box
// of a block, or a block that is not loaded: the box of the player, grown
// by margin to its sides and reaching depth below its feet.
func (p *Player) groundWithin(pos mgl64.Vec3, margin, depth float64) bool {
	box := p.H().Type().BBox(p).Translate(pos).GrowVec3(mgl64.Vec3{margin, 0, margin}).Extend(mgl64.Vec3{0, -depth, 0})
	low, high := cube.PosFromVec3(box.Min()), cube.PosFromVec3(box.Max())
	if p.chunksFailed(low, high) {
		// The blocks of a chunk that failed to load are not the ground the
		// client stands on.
		return true
	}
	for x := low[0]; x <= high[0]; x++ {
		for z := low[2]; z <= high[2]; z++ {
			for y := low[1]; y <= high[1]; y++ {
				bp := cube.Pos{x, y, z}
				b, loaded := p.tx.BlockLoaded(bp)
				if !loaded {
					// A block the server does not have may be the ground; the
					// void below the world is not.
					if !bp.OutOfBounds(p.tx.Range()) {
						return true
					}
					continue
				}
				for _, bb := range b.Model().BBox(bp, p.tx) {
					if bb.Translate(bp.Vec3()).IntersectsWith(box) {
						return true
					}
				}
			}
		}
	}
	return false
}

// chunksFailed reports whether a chunk that holds a block from low to high
// is the empty stand-in of one that failed to load.
func (p *Player) chunksFailed(low, high cube.Pos) bool {
	first, last := world.ChunkPosition(low), world.ChunkPosition(high)
	for x := first[0]; x <= last[0]; x++ {
		for z := first[1]; z <= last[1]; z++ {
			if p.tx.ChunkFailed(world.ChunkPos{x, z}) {
				return true
			}
		}
	}
	return false
}

// restEpsilon is the most a move may rise for a player to count as not
// rising: a client sends its position as a float32.
const restEpsilon = 5e-4

// clientGround reports whether the client of the player stood on the ground
// before its move, as the envelopes need it: whether it could jump. The
// server's own onGround is the box below the feet of the player, 0.05 deep,
// and holds for a player the blocks of both agree on. The blocks of a
// Bedrock client do not always: it stands on a layer of snow the server
// lets it sink through, and its position is a float32 that can lie a hair
// off the edge of a block. So a client also stands on the ground where its
// box, grown by Ascend.GroundMargin to its sides, touches a block, and, if
// its last move did not rise or it said it collided below in it, where a
// block is within Ascend.GroundDepth below it. A client that claims ground
// in mid-air, or after a jump, farther from the blocks than that, has none.
func (p *Player) clientGround(ac *acState) bool {
	if p.onGround {
		return true
	}
	conf := ac.conf.Ascend
	pos := p.Position()
	if p.groundWithin(pos, conf.GroundMargin, 0.05) {
		return true
	}
	return (ac.last.claim || ac.last.vertical <= restEpsilon) && p.groundWithin(pos, conf.GroundMargin, conf.GroundDepth)
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

// inLiquid reports whether the box of the player touches water or lava, in
// the chunks the world has loaded.
func (p *Player) inLiquid() bool {
	box := p.H().Type().BBox(p).Translate(p.Position()).Grow(-.001)
	for x := int(math.Floor(box.Min()[0])); x <= int(math.Floor(box.Max()[0])); x++ {
		for y := int(math.Floor(box.Min()[1])); y <= int(math.Floor(box.Max()[1])); y++ {
			for z := int(math.Floor(box.Min()[2])); z <= int(math.Floor(box.Max()[2])); z++ {
				pos := cube.Pos{x, y, z}
				// A liquid is looked up in the chunks the world has: asking for
				// one it does not would load it, and wait for its generation.
				if _, loaded := p.tx.BlockLoaded(pos); !loaded {
					continue
				}
				if _, ok := p.tx.Liquid(pos); ok {
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

// moveVerdict is what the movement checks made of a move: refused, the
// client set back or the player kicked; keepFall, the move is applied but
// does not reset the fall distance.
type moveVerdict struct {
	refused, keepFall bool
}

// checkMove runs the movement checks on a move of delta of the client,
// whose state before it is st. The first setback stops the checks; those
// that only log go on.
func (p *Player) checkMove(ac *acState, m client.Move, delta mgl64.Vec3, st anticheat.State) moveVerdict {
	if !p.movementChecked(ac, m) {
		return moveVerdict{}
	}
	// The checks that judge a move against the blocks wait for the chunks
	// they would read: where the server lacks the blocks a client stands on,
	// it cannot tell it from a cheat.
	trusted := p.blocksTrusted(m)
	if !trusted {
		ac.hover = 0
	}
	// NoClip first: a move into blocks is set back before it is judged by
	// how fast it went.
	if trusted && p.checkNoClip(ac, m) || p.checkSpeed(ac, m, delta, st) || trusted && p.checkFly(ac, m, delta) {
		return moveVerdict{refused: true}
	}
	verdict := p.checkAscend(ac, m, delta, st)
	if verdict.refused || trusted && p.checkPhase(ac, m, delta) {
		return moveVerdict{refused: true}
	}
	return verdict
}

// maxTrustedChunks is how many chunks wide a move may be for blocksTrusted
// to look at the chunks it crosses: a longer one is not a walk.
const maxTrustedChunks = 4

// blocksTrusted reports whether the server has the blocks that judging the
// move m of the player reads: every chunk the box of the player touches,
// before and after the move and a block around, is loaded, generated, and
// not the empty stand-in of a chunk that failed to load. A move too long to
// be a walk is not.
func (p *Player) blocksTrusted(m client.Move) bool {
	box := p.H().Type().BBox(p)
	from, to := p.Position(), m.Position
	lo := cube.PosFromVec3(mgl64.Vec3{min(from[0], to[0]) + box.Min()[0] - 1, 0, min(from[2], to[2]) + box.Min()[2] - 1})
	hi := cube.PosFromVec3(mgl64.Vec3{max(from[0], to[0]) + box.Max()[0] + 1, 0, max(from[2], to[2]) + box.Max()[2] + 1})
	first, last := world.ChunkPosition(lo), world.ChunkPosition(hi)
	if last[0]-first[0] >= maxTrustedChunks || last[1]-first[1] >= maxTrustedChunks {
		return false
	}
	for x := first[0]; x <= last[0]; x++ {
		for z := first[1]; z <= last[1]; z++ {
			pos := world.ChunkPos{x, z}
			if loaded, ready := p.tx.ChunkState(pos); !loaded || !ready || p.tx.ChunkFailed(pos) {
				return false
			}
		}
	}
	return true
}

// ascendJudged reports whether Ascend judges a move m of the player: the
// movement checks do, the player is not exempt, and it may not fly and
// neither flies nor glides.
func (p *Player) ascendJudged(ac *acState, m client.Move) bool {
	return p.movementChecked(ac, m) && p.checks(ac, anticheat.Ascend) && !p.GameMode().AllowsFlying() && !p.Flying() && !p.Gliding()
}

// checkAscend holds the vertical part of a move to the envelope of a jump,
// a step, a climb, a rise in a liquid, a bounce or an impulse, with the
// epsilon of Ascend, and catches the up-blips of NoFall: a rise while
// nothing holds the player up and gravity has it fall. A move that ends on
// the ground may step up StepHeight, as Java steps up a falling player
// that lands. With the action cancel, the move is applied but does not
// reset the fall distance, as a NoFall up-blip would.
func (p *Player) checkAscend(ac *acState, m client.Move, delta mgl64.Vec3, st anticheat.State) moveVerdict {
	dy := delta[1]
	if dy <= 0 || !p.ascendJudged(ac, m) {
		return moveVerdict{}
	}
	eps := ac.conf.Ascend.Epsilon
	allowed := anticheat.VerticalAllowance(st)
	landed := dy <= st.Step && p.groundAt(m.Position)
	if landed {
		allowed = max(allowed, st.Step)
	}
	var act anticheat.Action
	switch {
	case dy > allowed+eps:
		act = p.violate(ac, anticheat.Ascend, "envelope", dy-allowed-eps, fmt.Sprintf("%.3f > %.3f", dy, allowed+eps))
	case !landed && !st.Supported() && st.Impulse[1] <= 0 && anticheat.AirborneVertical(st) < -eps:
		act = p.violate(ac, anticheat.Ascend, "blip", dy, fmt.Sprintf("+%.3f falling at %.3f", dy, anticheat.AirborneVertical(st)))
	default:
		return moveVerdict{}
	}
	switch act {
	case anticheat.Cancel:
		return moveVerdict{keepFall: true}
	case anticheat.Setback, anticheat.Kick:
		return moveVerdict{refused: p.actOnMove(ac, m, act, p.Position())}
	}
	return moveVerdict{}
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

// recordMove keeps what the movement checks need of the move m, of delta,
// the client made, from the state st before it. If an envelope judged the move
// (speed, ascend), the next move carries what physics allowed of it at
// most, the envelope with no tolerance: a move past it that was only
// logged does not lift the envelopes after it, so that the tolerance does
// not compound tick after tick. A move the envelope did not judge, as in
// flight or in a grace period, is carried as it was.
func (ac *acState) recordMove(m client.Move, delta mgl64.Vec3, st anticheat.State, speed, ascend bool) {
	h, v := math.Hypot(delta[0], delta[2]), delta[1]
	if speed {
		h = min(h, anticheat.HorizontalAllowance(st))
	}
	if ascend && v > 0 {
		v = min(v, max(anticheat.VerticalAllowance(st), st.Step))
	}
	ac.last = lastMove{
		horizontal:   h,
		vertical:     v,
		prevVertical: ac.last.vertical,
		ground:       st.Ground,
		liquid:       st.Liquid,
		friction:     st.Friction,
		lenient:      st.Lenient,
		claim:        m.ClaimsGround && delta[1] <= st.Step,
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

// clipBox returns the box of the player that NoClip and Phase judge a move
// of the client m with: that of its pose, or the lower one the client
// claims to start in the move, as the flags of a move are handled after it.
func (p *Player) clipBox(m client.Move) cube.BBox {
	box := p.H().Type().BBox(p)
	s := p.Scale()
	switch {
	case m.Crawling || m.Swimming || m.Gliding:
		box = cube.Box(-0.3*s, 0, -0.3*s, 0.3*s, min(box.Max()[1], 0.6*s), 0.3*s)
	case m.Sneaking:
		box = cube.Box(-0.3*s, 0, -0.3*s, 0.3*s, min(box.Max()[1], 1.49*s), 0.3*s)
	}
	return box
}

// solidBoxes calls f with the collision boxes of the blocks within box that
// NoClip and Phase judge, translated to the world, and the position of
// their block. Blocks whose shape depends on who touches them (scaffolding,
// powder snow, cobweb), blocks without collision and blocks a piston moves
// are left out, and so are unloaded blocks. f returns false to stop.
func (p *Player) solidBoxes(box cube.BBox, f func(pos cube.Pos, bb cube.BBox) bool) {
	low, high := cube.PosFromVec3(box.Min()), cube.PosFromVec3(box.Max())
	for x := low[0]; x <= high[0]; x++ {
		for y := low[1]; y <= high[1]; y++ {
			for z := low[2]; z <= high[2]; z++ {
				pos := cube.Pos{x, y, z}
				b, loaded := p.tx.BlockLoaded(pos)
				if !loaded {
					continue
				}
				switch b.(type) {
				case block.Air, block.Scaffolding, block.PowderSnow, block.Cobweb, block.MovingBlock:
					continue
				}
				for _, bb := range b.Model().BBox(pos, p.tx) {
					if !f(pos, bb.Translate(pos.Vec3())) {
						return
					}
				}
			}
		}
	}
}

// checkNoClip refuses a move into blocks: as Java's
// isEntityCollidingWithAnythingNew, a move whose box, shrunk by MinDepth,
// enters the box of a block the box before the move, shrunk alike, was not
// in. With
// ResendFirst, the blocks entered are resent, so that a client that walked
// into a block it did not have is shown it: while the level is at most
// ActAt, the move is only logged, and a cheat is set back from the next
// violation. A player without collision is not judged.
func (p *Player) checkNoClip(ac *acState, m client.Move) (refused bool) {
	if !p.checks(ac, anticheat.NoClip) || !p.GameMode().HasCollision() {
		return false
	}
	// Both boxes shrink by MinDepth: a box that was in a block by less than
	// that did not enter it, and its next move in may.
	shape := p.clipBox(m)
	before := shape.Translate(p.Position()).Grow(-ac.conf.NoClip.MinDepth)
	after := shape.Translate(m.Position).Grow(-ac.conf.NoClip.MinDepth)
	if after.Width() <= 0 || after.Height() <= 0 || after.Length() <= 0 {
		return false
	}
	var entered []cube.Pos
	p.solidBoxes(after, func(pos cube.Pos, bb cube.BBox) bool {
		if bb.IntersectsWith(after) && !bb.IntersectsWith(before) && (len(entered) == 0 || entered[len(entered)-1] != pos) {
			entered = append(entered, pos)
		}
		return true
	})
	if len(entered) == 0 {
		return false
	}
	name, _ := p.tx.Block(entered[0]).EncodeBlock()
	act := p.violate(ac, anticheat.NoClip, "enter", float64(len(entered)), fmt.Sprintf("into %s at %v", name, entered[0]))
	if ac.conf.NoClip.ResendFirst {
		for _, pos := range entered {
			p.resendNearbyBlock(pos)
		}
	}
	return p.actOnMove(ac, m, act, p.Position())
}

// checkPhase finds a move through what blocks its path, as Java's "moved
// wrongly": the collisions of the path, swept from where the server has the
// player, stop it farther than Tolerance from where it moved, horizontally,
// though the box before the move was free. A player on the ground may step
// up StepHeight on the way. It runs only on moves longer than 0.05 that
// NoClip let through, and not while an impulse the client may still apply
// pushes the player, Java's post-impulse grace.
func (p *Player) checkPhase(ac *acState, m client.Move, delta mgl64.Vec3) (refused bool) {
	if !p.checks(ac, anticheat.Phase) || !p.GameMode().HasCollision() || delta.Len() <= 0.05 ||
		p.pendingImpulse(ac) != (mgl64.Vec3{}) {
		return false
	}
	pos := p.Position()
	before := p.H().Type().BBox(p).Translate(pos).Grow(-1e-5)
	free := true
	p.solidBoxes(before, func(_ cube.Pos, bb cube.BBox) bool {
		free = !bb.IntersectsWith(before)
		return free
	})
	if !free {
		return false
	}
	got := p.sweep(ac, pos, delta)
	miss := math.Hypot(delta[0]-got[0], delta[2]-got[2])
	if miss <= ac.conf.Phase.Tolerance {
		return false
	}
	return p.actOnMove(ac, m, p.violate(ac, anticheat.Phase, "path", miss, fmt.Sprintf("stopped %.2f short", miss)), pos)
}

// sweep returns how far a move of delta from pos gets through the
// collisions of the blocks: straight, or stepping up StepHeight if the
// player stood on the ground and that goes farther.
func (p *Player) sweep(ac *acState, pos, delta mgl64.Vec3) mgl64.Vec3 {
	got, _ := ac.mc.CheckCollision(p.tx, p, pos, delta)
	if !p.onGround || (math.Abs(got[0]-delta[0]) < 1e-7 && math.Abs(got[2]-delta[2]) < 1e-7) {
		return got
	}
	step := ac.conf.Ascend.StepHeight
	up, _ := ac.mc.CheckCollision(p.tx, p, pos, mgl64.Vec3{0, step, 0})
	across, _ := ac.mc.CheckCollision(p.tx, p, pos.Add(up), mgl64.Vec3{delta[0], 0, delta[2]})
	down, _ := ac.mc.CheckCollision(p.tx, p, pos.Add(up).Add(across), mgl64.Vec3{0, -up[1] + min(delta[1], 0), 0})
	if stepped := up.Add(across).Add(down); stepped[0]*stepped[0]+stepped[2]*stepped[2] > got[0]*got[0]+got[2]*got[2] {
		return stepped
	}
	return got
}
