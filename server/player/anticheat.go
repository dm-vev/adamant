package player

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/go-gl/mathgl/mgl64"
)

// ClientMove moves the player as its client moved it in one of its ticks:
// to m.Position, looking along m.Rotation. The game refuses a move no
// player makes in a tick, farther than 10 blocks (17 gliding) beyond the
// velocity the server knows, as Java does, and puts the client back with
// Self.CorrectMove. A move awaiting the acknowledgement of a teleport, of a
// player that rides, or that changes nothing, is ignored; a player that
// rides only turns.
//
// Plugins move a player with Move, which the game does not check.
func (p *Player) ClientMove(m client.Move) client.MoveResult {
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
	if mgl64.FloatEqual(delta.Len(), 0) && mgl64.FloatEqual(deltaYaw, 0) && mgl64.FloatEqual(deltaPitch, 0) {
		return client.MoveIgnored
	}
	if p.movedTooQuickly(delta) {
		// As Java does, a move no player makes in a tick puts the client
		// back where the server has it, rather than taking it anywhere.
		p.log.Debug("client move refused: moved too quickly", "delta", fmt.Sprint(delta))
		p.client().CorrectMove(p, pos, mgl64.Vec3{}, m.Tick)
		return client.MoveRefused
	}
	p.Move(delta, deltaYaw, deltaPitch)
	return client.MoveApplied
}

// movedTooQuickly reports a move farther than a player goes in a tick, as
// Java judges it: by more than 10 blocks, or 17 gliding, beyond what the
// velocity of the player covers.
func (p *Player) movedTooQuickly(delta mgl64.Vec3) bool {
	limit := 100.0
	if p.Gliding() {
		limit = 300
	}
	return delta.LenSqr()-p.Velocity().LenSqr() > limit
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
