package session

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
)

// Bytes the connection holds for its client, sent or not yet taken, past
// which the session is congested, and below which it is no longer. A
// congested session keeps back the moves of other entities, as a client
// behind does better with where they are once it caught up than with
// every step they took meanwhile, and drops sounds, particles and
// animations; what changes the world, its chunks and blocks, the
// entities that come and go, and the player itself, still goes.
const congestedAbove, clearBelow = 256 << 10, 64 << 10

// keepBack reports whether a move of e is kept back from a congested
// client, recording e to be shown where it is once the congestion clears.
// The player of the session is never kept back.
func (s *Session) keepBack(e world.Entity) bool {
	if !s.congested.Load() || e.H() == s.ent {
		return false
	}
	s.staleMu.Lock()
	if s.stale == nil {
		s.stale = make(map[*world.EntityHandle]struct{})
	}
	s.stale[e.H()] = struct{}{}
	s.staleMu.Unlock()
	movesKeptBack.Inc()
	return true
}

// dropCosmetic reports whether a sound, a particle or an animation is
// dropped for a congested client.
func (s *Session) dropCosmetic() bool {
	if !s.congested.Load() {
		return false
	}
	cosmeticsDropped.Inc()
	return true
}

// updateCongestion reads what the connection holds for its client, each
// tick before the world moves anything: it marks the session congested past
// congestedAbove, and once it is below clearBelow again, shows the
// entities whose moves were kept back where they are now.
func (s *Session) updateCongestion(tx *world.Tx) {
	b, ok := s.conn.(interface{ Buffered() int })
	if !ok {
		return
	}
	n := b.Buffered()
	if !s.congested.Load() {
		if n > congestedAbove {
			s.congested.Store(true)
			congestions.Inc()
		}
		return
	}
	if n >= clearBelow {
		return
	}
	s.congested.Store(false)
	s.staleMu.Lock()
	stale := s.stale
	s.stale = nil
	s.staleMu.Unlock()
	for h := range stale {
		e, ok := h.Entity(tx)
		if !ok || s.entityHidden(e) {
			continue
		}
		s.entityMutex.RLock()
		_, viewed := s.entityRuntimeIDs[h]
		s.entityMutex.RUnlock()
		if !viewed {
			continue
		}
		id := s.entityRuntimeID(e)
		onGround := false
		if g, ok := e.(interface{ OnGround() bool }); ok {
			onGround = g.OnGround()
		}
		s.writePacket(absoluteMove(id, e, e.Position(), e.Rotation(), onGround, false))
		if v, ok := e.(interface{ Velocity() mgl64.Vec3 }); ok {
			s.writePacket(&v2193.SetActorMotionPacket{EntityRuntimeID: id, Velocity: vec64To32(v.Velocity())})
		}
		staleShown.Inc()
	}
}
