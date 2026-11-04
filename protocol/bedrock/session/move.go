package session

import (
	"github.com/df-mc/dragonfly/protocol/bedrock/netconn"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl64"
)

// sharedMove is what the viewers of a world.EntityMove share of it: its
// packets, encoded once, either of which may be nil.
type sharedMove struct {
	delta, motion *netconn.Shared
}

// ViewEntityMove ...
func (s *Session) ViewEntityMove(e world.Entity, m *world.EntityMove) {
	if e.H() == s.ent {
		if m.Absolute && !s.moving.Load() {
			// The server moved the player of the session, not its client.
			s.writePacket(absoluteMove(selfEntityRuntimeID, e, m.Pos, m.Rot, m.OnGround, false))
		}
		return
	}
	if s.entityHidden(e) || s.keepBack(e) {
		return
	}
	id := s.entityRuntimeID(e)
	if m.Absolute {
		shared, _ := m.Shared.(*netconn.Shared)
		if shared == nil {
			shared = &netconn.Shared{Packet: absoluteMove(id, e, m.Pos, m.Rot, m.OnGround, false)}
			m.Shared = shared
		}
		s.writePacket(shared)
		return
	}
	// A move may change the velocity alone, which goes without a delta.
	delta := m.Changed&^world.MoveVelocity != 0
	shared, _ := m.Shared.(*sharedMove)
	if shared == nil {
		pk, motion := moveActorDelta(id, e, m, m.Changed)
		shared = &sharedMove{}
		if delta {
			shared.delta = &netconn.Shared{Packet: pk}
		}
		if motion != nil {
			shared.motion = &netconn.Shared{Packet: motion}
		}
		m.Shared = shared
	}
	if shared.delta != nil {
		s.writePacket(shared.delta)
	}
	if shared.motion != nil {
		s.writePacket(shared.motion)
	}
}

// ClientMoving reports whether the session is applying a move its client
// made, which the client is not shown again.
func (s *Session) ClientMoving() bool {
	return s != Nop && s.moving.Load()
}

// HandlingInput reports whether the session is handling a packet of its
// client.
func (s *Session) HandlingInput() bool {
	return s != Nop && s.handlingInput.Load()
}

// CorrectMove puts the player of the session back at pos, moving at vel: a
// teleport, which the client acknowledges with a move to pos before its
// moves count again.
func (s *Session) CorrectMove(c Controllable, pos, vel mgl64.Vec3, _ uint64) {
	s.ViewEntityTeleport(c, pos)
	if vel != (mgl64.Vec3{}) {
		s.writePacket(&mc.SetActorMotionPacket{EntityRuntimeID: selfEntityRuntimeID, Velocity: vec64To32(vel)})
	}
}

// moveActorDelta returns the MoveActorDelta of the parts of a move, and the
// SetActorMotion of its velocity if those parts hold it.
func moveActorDelta(id uint64, e world.Entity, m *world.EntityMove, parts world.MoveParts) (delta, motion proto.Packet) {
	pos := vec64To32(entityNetworkPosition(e, m.Pos))
	pk := &mc.MoveActorDeltaPacket{EntityRuntimeID: id, OnGround: m.OnGround, Ticks: uint64(m.Ticks)}
	if parts&world.MoveX != 0 {
		pk.PositionX = proto.Some(pos[0])
	}
	if parts&world.MoveY != 0 {
		pk.PositionY = proto.Some(pos[1])
	}
	if parts&world.MoveZ != 0 {
		pk.PositionZ = proto.Some(pos[2])
	}
	if parts&world.MovePitch != 0 {
		pk.RotationX = proto.Some(float32(m.Rot.Pitch()))
	}
	if parts&world.MoveYaw != 0 {
		pk.RotationY = proto.Some(float32(m.Rot.Yaw()))
	}
	if parts&world.MoveHeadYaw != 0 {
		pk.RotationYHead = proto.Some(float32(m.HeadYaw))
	}
	if parts&world.MoveVelocity != 0 {
		motion = &mc.SetActorMotionPacket{EntityRuntimeID: id, Velocity: vec64To32(m.Vel)}
	}
	return pk, motion
}
