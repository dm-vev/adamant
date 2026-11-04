package player

import (
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// ClientMove moves the player as its client moved it in one of its ticks:
// to m.Position, looking along m.Rotation, unless the anti-cheat refuses
// the move. A move awaiting the acknowledgement of a teleport, or that
// changes nothing, is ignored; a player that rides only turns.
//
// The game refuses a move no player makes in a tick, farther than 10
// blocks (17 gliding) beyond the velocity the server knows, as Java does,
// and puts the client back with Self.CorrectMove: the MovedTooQuickly check
// of the anti-cheat, or always for a player that is not checked.
//
// Plugins move a player with Move, which the game does not check.
func (p *Player) ClientMove(m client.Move) client.MoveResult {
	ac := p.anticheat()
	if ac != nil {
		p.noteClientMove(ac, m)
	}
	if m.AwaitingTeleport {
		return client.MoveIgnored
	}
	pos, rot := p.Position(), p.Rotation()
	delta := m.Position.Sub(pos)
	deltaYaw, deltaPitch := m.Rotation.Yaw()-rot.Yaw(), m.Rotation.Pitch()-rot.Pitch()
	if m.Riding {
		// The player steers what it rides rather than move itself.
		delta = mgl64.Vec3{}
	}
	unchanged := mgl64.FloatEqual(delta.Len(), 0) && mgl64.FloatEqual(deltaYaw, 0) && mgl64.FloatEqual(deltaPitch, 0)
	if unchanged && ac == nil {
		return client.MoveIgnored
	}
	if !unchanged && p.refuseTooQuick(ac, m, delta) {
		return client.MoveRefused
	}
	if ac == nil {
		p.Move(delta, deltaYaw, deltaPitch)
		return client.MoveApplied
	}
	// A client sends a move every tick, moved or not: the movement checks
	// judge each, as hovering in place moves nothing.
	st := p.moveState(ac, delta)
	speed, ascend := p.speedJudged(ac, m), p.ascendJudged(ac, m)
	verdict := p.checkMove(ac, m, delta, st)
	if verdict.refused {
		return client.MoveRefused
	}
	if unchanged {
		ac.recordMove(delta, st, speed, ascend)
		return client.MoveIgnored
	}
	fall := p.fallDistance
	ac.clientMove = true
	p.Move(delta, deltaYaw, deltaPitch)
	ac.clientMove = false
	if verdict.keepFall {
		// Ascend cancels an up-blip: it does not reset the fall distance.
		p.fallDistance = fall
	}
	ac.recordMove(delta, st, speed, ascend)
	return client.MoveApplied
}

// noteClientMove starts the grace periods a move of the client tells of:
// the first move after the player spawned, a loading screen, and the
// acknowledgement of a teleport.
func (p *Player) noteClientMove(ac *acState, m client.Move) {
	g := ac.conf.Grace
	switch {
	case !ac.moved:
		ac.moved = true
		p.startGrace(g.Join)
	case m.Loading:
		p.startGrace(max(g.Join, g.WorldChange))
	case m.Acknowledged && ac.setbackPending:
		ac.setbackPending = false
	case m.Acknowledged:
		p.startGrace(g.Teleport)
	}
}

// refuseTooQuick refuses a move farther than a player goes in a tick, as
// Java's "moved too quickly" does: it reports whether the move was refused
// and the client put back, or the player kicked.
func (p *Player) refuseTooQuick(ac *acState, m client.Move, delta mgl64.Vec3) bool {
	tooQuick, dist, bound := p.movedTooQuickly(ac, delta)
	if !tooQuick {
		return false
	}
	pos := p.Position()
	if ac == nil {
		// A player that is not checked is held to Java's bound alone.
		p.log.Debug("client move refused: moved too quickly", "delta", fmt.Sprint(delta))
		p.client().CorrectMove(p, pos, mgl64.Vec3{}, m.Tick)
		return true
	}
	if m.Loading || !p.checks(ac, anticheat.MovedTooQuickly) || !p.movementCheckRule() {
		return false
	}
	switch p.violate(ac, anticheat.MovedTooQuickly, "distance", dist-bound, fmt.Sprintf("%.2f > %.2f", dist, bound)) {
	case anticheat.Setback:
		p.setBack(ac, pos, m.Tick)
		return true
	case anticheat.Kick:
		return true
	}
	return false
}

// movementCheckRule reports whether the game rules of the world have Java's
// movement checks on: player_movement_check, and elytra_movement_check
// while gliding.
func (p *Player) movementCheckRule() bool {
	w := p.tx.World()
	if !w.GameRuleBool("player_movement_check") {
		return false
	}
	return !p.Gliding() || w.GameRuleBool("elytra_movement_check")
}

// setBack puts the client of the player back at pos for the move of the
// client tick passed, and forgets what the movement checks know of its
// moves. At pos the server has the player, or, for Fly, it last stood,
// where its viewers see it put back. Unlike a teleport, a setback starts no
// grace: the moves after its acknowledgement are checked.
func (p *Player) setBack(ac *acState, pos mgl64.Vec3, tick uint64) {
	if pos != p.Position() {
		viewers, release := p.viewers()
		for _, v := range viewers {
			if v != world.Viewer(p.c) {
				v.ViewEntityTeleport(p, pos)
			}
		}
		releaseBorrowedViewers(p.tx, viewers, release)
		p.setPosition(pos)
		p.data.Vel = mgl64.Vec3{}
		p.ResetFallDistance()
	}
	p.client().CorrectMove(p, pos, mgl64.Vec3{}, tick)
	ac.setbackPending = true
	ac.resetMovement()
}

// movedTooQuickly reports a move farther than a player goes in a tick, as
// Java judges it: by more than 10 blocks, or 17 gliding, beyond what the
// velocity of the player covers, and returns the distance moved and the
// bound. The anti-cheat sets the limits, and has the velocity cover the
// largest impulse the client may still apply too, so that an explosion that
// throws a player farther than 10 blocks a tick is no reason to set it
// back.
func (p *Player) movedTooQuickly(ac *acState, delta mgl64.Vec3) (tooQuick bool, dist, bound float64) {
	limit := 10.0
	if p.Gliding() {
		limit = math.Sqrt(300)
	}
	expected := p.Velocity().Len()
	if ac != nil {
		limit = ac.conf.MovedTooQuickly.Limit
		if p.Gliding() {
			limit = ac.conf.MovedTooQuickly.LimitGliding
		}
		expected += p.pendingImpulse(ac).Len()
	}
	dist = delta.Len()
	return delta.LenSqr()-expected*expected > limit*limit, dist, math.Sqrt(limit*limit + expected*expected)
}

// DestroyBlock breaks the block at pos at once, as the client of the player
// did: in creative, or a block that breaks in a tick.
func (p *Player) DestroyBlock(pos cube.Pos) {
	p.BreakBlock(pos)
}

// clientInput reports whether what the player is asked comes from its
// client, which the game checks.
func (p *Player) clientInput() bool {
	return p.c != nil && p.c.HandlingInput()
}

// beginAction starts a checked action of the player. It returns the
// anti-cheat state if the action comes from its client and runs in no other
// checked action, nil otherwise, and the function that ends the action,
// which must be called once it ends.
func (p *Player) beginAction() (*acState, func()) {
	ac := p.anticheat()
	if ac == nil {
		return nil, func() {}
	}
	ac.depth++
	end := func() { ac.depth-- }
	if ac.depth > 1 || !p.clientInput() {
		return nil, end
	}
	return ac, end
}
