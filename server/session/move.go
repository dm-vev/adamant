package session

import (
	"sync"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// sharedPacket is a packet that goes alike to every client it is written
// to: the first connection to write it encodes it, the others copy those
// bytes, so that what many clients are shown is encoded once.
type sharedPacket struct {
	proto.Packet
	once sync.Once
	b    []byte
}

func (p *sharedPacket) Encode(w *wire.Writer) {
	p.once.Do(func() {
		var e wire.Writer
		p.Packet.Encode(&e)
		p.b = e.B
	})
	w.B = append(w.B, p.b...)
}

// sharedMove is what the viewers of a world.EntityMove share of it: its
// packets, encoded once, either of which may be nil.
type sharedMove struct {
	delta, motion *sharedPacket
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
	if s.entityHidden(e) {
		return
	}
	id := s.entityRuntimeID(e)
	if m.Absolute {
		shared, _ := m.Shared.(*sharedPacket)
		if shared == nil {
			shared = &sharedPacket{Packet: absoluteMove(id, e, m.Pos, m.Rot, m.OnGround, false)}
			m.Shared = shared
		}
		s.writePacket(shared)
		return
	}
	// A move may change the velocity alone, which goes without a delta.
	delta := m.Changed&^world.MoveVelocity != 0
	if delta && s.takeFreshMove(e.H()) {
		// The client has the entity where it was when shown, which the moves
		// shown since may have left: it is shown all of this one.
		pk, motion := moveActorDelta(id, e, m, world.MoveAll|m.Changed)
		s.writePacket(pk)
		if motion != nil {
			s.writePacket(motion)
		}
		return
	}
	shared, _ := m.Shared.(*sharedMove)
	if shared == nil {
		pk, motion := moveActorDelta(id, e, m, m.Changed)
		shared = &sharedMove{}
		if delta {
			shared.delta = &sharedPacket{Packet: pk}
		}
		if motion != nil {
			shared.motion = &sharedPacket{Packet: motion}
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

// takeFreshMove reports whether the entity of a handle was shown since the
// last move of it the session was shown, and notes it moved.
func (s *Session) takeFreshMove(h *world.EntityHandle) bool {
	s.entityMutex.RLock()
	_, fresh := s.freshMoves[h]
	s.entityMutex.RUnlock()
	if fresh {
		s.entityMutex.Lock()
		delete(s.freshMoves, h)
		s.entityMutex.Unlock()
	}
	return fresh
}

// moveActorDelta returns the MoveActorDelta of the parts of a move, and the
// SetActorMotion of its velocity if those parts hold it.
func moveActorDelta(id uint64, e world.Entity, m *world.EntityMove, parts world.MoveParts) (delta, motion proto.Packet) {
	pos := vec64To32(entityNetworkPosition(e, m.Pos))
	pk := &v2193.MoveActorDeltaPacket{EntityRuntimeID: id, OnGround: m.OnGround, Ticks: uint64(m.Ticks)}
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
		motion = &v2193.SetActorMotionPacket{EntityRuntimeID: id, Velocity: vec64To32(m.Vel)}
	}
	return pk, motion
}
