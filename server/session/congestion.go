package session

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
)

// Chunks wait while the connection queues more for its client than its
// path carries: a congestion window, or what the path carries in a tick at
// that window and the round trip measured if more. The packets of the game
// behind them would wait as long; the sctp package measures the bound in
// TestLossProfile. chunkTick is the tick of a world. chunkCompression is
// how many times smaller chunks go out than their bytes: flate at level 1,
// as batches go, shrinks the sub-chunks of a generated column 3.5 times; a
// little less is taken, so the estimate errs long. Past maxQueuedSubChunks
// offsets queued, the oldest requests are answered whatever the room, so a
// client that asks without end cannot grow the queue: the send buffer of
// the connection bounds it then.
const (
	chunkTick          = time.Second / 20
	chunkCompression   = 3
	maxQueuedSubChunks = 16 << 10
)

// sendQueue is a connection that tells what it queued for its client: the
// bytes waiting behind those in flight, and the congestion window and
// round trip time of its path, a window of zero if it has none.
type sendQueue interface {
	SendQueue() (queued, window int, rtt time.Duration)
}

// connRoom returns the bytes the connection may queue for its client this
// tick past those it holds: see chunkTick. A connection that does not tell
// has room without bound.
func (s *Session) connRoom() int {
	q, ok := s.conn.(sendQueue)
	if !ok {
		return math.MaxInt
	}
	queued, window, rtt := q.SendQueue()
	if window <= 0 {
		return math.MaxInt
	}
	bound := window
	if rtt > 0 {
		bound = max(bound, int(int64(window)*int64(chunkTick)/int64(rtt)))
	}
	return bound - queued
}

// chunkRoom returns what is left of room, the room of the connection, once
// the chunks written this tick and the blobs the client is to ask for are
// counted as they go out.
func (s *Session) chunkRoom(room int) int {
	owed := s.chunkBytes
	s.blobMu.Lock()
	for _, b := range s.blobs {
		owed += len(b)
	}
	s.blobMu.Unlock()
	return room - owed/chunkCompression
}

// sendSubChunks answers the sub-chunk requests queued, oldest first, while
// the chunks of the tick leave room, and past maxQueuedSubChunks whatever
// the room.
func (s *Session) sendSubChunks(tx *world.Tx, room int) {
	for len(s.subChunkQueue) > 0 && (s.subChunkOffsets > maxQueuedSubChunks || s.chunkRoom(room) > 0) {
		pk := s.subChunkQueue[0]
		s.subChunkQueue[0] = nil
		s.subChunkQueue = s.subChunkQueue[1:]
		s.subChunkOffsets -= len(pk.Offsets)
		s.chunkBytes += s.answerSubChunks(pk, tx)
	}
}

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
