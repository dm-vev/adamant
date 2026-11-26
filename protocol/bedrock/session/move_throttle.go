package session

import (
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
)

// Non-authoritative entity movement packets (MoveActorAbsolute from
// ViewEntityMovement and ViewEntityMove, and MoveActorDelta from
// ViewEntityMove) are throttled by 3D squared distance from the viewer
// player. Within 32 blocks, every movement update goes out at full tick rate
// so PvP, close projectiles, and interactions stay crisp. Beyond 32 blocks,
// the Bedrock client smoothly interpolates sparser updates:
//   - <= 32 blocks:  1x (no throttling)
//   - <= 64 blocks:  2x (every 2nd update)
//   - <= 128 blocks: 4x (every 4th update)
//   - > 128 blocks:  8x (every 8th update)
//
// Authoritative moves (ViewEntityDisplacement, ViewEntityTeleport), the
// viewer's own entity (selfEntityRuntimeID), onGround transitions, and jumps
// exceeding 4 blocks since the last sent position bypass throttling
// immediately. The leading edge of movement goes out immediately, and any
// suppressed move is flushed on the trailing edge in tickMoveThrottle once
// the entity's interval elapses without another move, so a distant entity
// that stops moving always settles at its exact final position.
const (
	moveThrottleNearDistSqr = 32 * 32
	moveThrottleMidDistSqr  = 64 * 64
	moveThrottleFarDistSqr  = 128 * 128

	moveThrottleJumpDistSqr = 4 * 4
)

// moveThrottleState holds the per-entity throttling state for a distant
// entity seen by a session.
type moveThrottleState struct {
	lastSentTick     uint64
	lastSentPos      mgl64.Vec3
	lastSentOnGround bool

	pending         bool
	pendingInterval uint64
	pendingRuntime  uint64
	pendingWorldPos mgl64.Vec3
	pendingPos      mgl32.Vec3
	pendingRot      mgl32.Vec3
	pendingOnGround bool
	pendingParts    world.MoveParts
}

// throttledFlushPacket holds the network-ready fields of a pending throttled
// move to be written after releasing moveThrottleMu.
type throttledFlushPacket struct {
	runtimeID uint64
	pos       mgl32.Vec3
	rot       mgl32.Vec3
	onGround  bool
}

// moveThrottleFactor returns the movement interval multiplier for an entity
// at pos relative to the session's viewer position: 1 within 32 blocks (or
// before viewPos is known), 2 within 64, 4 within 128, and 8 beyond 128.
func (s *Session) moveThrottleFactor(pos mgl64.Vec3) uint64 {
	viewPos := s.viewPos.Load()
	if viewPos == nil {
		return 1
	}
	d := pos.Sub(*viewPos)
	distSqr := d[0]*d[0] + d[1]*d[1] + d[2]*d[2]
	switch {
	case distSqr <= moveThrottleNearDistSqr:
		return 1
	case distSqr <= moveThrottleMidDistSqr:
		return 2
	case distSqr <= moveThrottleFarDistSqr:
		return 4
	default:
		return 8
	}
}

// throttleMove decides whether a non-authoritative move of e should be
// suppressed based on distance from the viewer. When suppress is false,
// extraParts contains any MoveParts accumulated during earlier suppressed
// delta moves that must be included in the outgoing packet.
func (s *Session) throttleMove(
	id uint64,
	e world.Entity,
	pos mgl64.Vec3,
	rot cube.Rotation,
	headYaw float64,
	hasExplicitHeadYaw bool,
	onGround bool,
	parts world.MoveParts,
	baseTicks int,
) (suppress bool, extraParts world.MoveParts) {
	h := e.H()
	if id == selfEntityRuntimeID || h == s.ent {
		return false, 0
	}
	factor := s.moveThrottleFactor(pos)
	if factor <= 1 {
		if !s.hasThrottled.Load() {
			return false, 0
		}
		return false, s.clearThrottledClose(h)
	}
	if baseTicks < 1 {
		baseTicks = 1
	}
	interval := factor * uint64(baseTicks)
	now := s.moveTick.Load()

	s.moveThrottleMu.Lock()
	st, exists := s.moveThrottle[h]
	sendNow := !exists ||
		(now > st.lastSentTick && now-st.lastSentTick >= interval) ||
		onGround != st.lastSentOnGround ||
		parts&world.MoveGround != 0 ||
		pos.Sub(st.lastSentPos).LenSqr() > moveThrottleJumpDistSqr
	if sendNow {
		var merged world.MoveParts
		if exists && st.pending {
			merged = st.pendingParts
		}
		if s.moveThrottle == nil {
			s.moveThrottle = make(map[*world.EntityHandle]moveThrottleState)
		}
		s.moveThrottle[h] = moveThrottleState{
			lastSentTick:     now,
			lastSentPos:      pos,
			lastSentOnGround: onGround,
			pendingInterval:  interval,
		}
		s.hasThrottled.Store(true)
		s.moveThrottleMu.Unlock()
		return false, merged
	}
	st.pending = true
	st.pendingInterval = interval
	st.pendingRuntime = id
	st.pendingWorldPos = pos
	st.pendingPos = vec64To32(entityNetworkPosition(e, pos))
	st.pendingRot = throttledNetworkRotation(e, rot, headYaw, hasExplicitHeadYaw)
	st.pendingOnGround = onGround
	st.pendingParts |= parts
	s.moveThrottle[h] = st
	s.hasPendingThrottled.Store(true)
	s.moveThrottleMu.Unlock()
	movesThrottled.Inc()
	return true, 0
}

// throttledNetworkRotation computes the wire rotation vector {pitch, yaw, headYaw}
// for a suppressed movement saved for trailing-edge flush.
func throttledNetworkRotation(e world.Entity, rot cube.Rotation, headYaw float64, explicit bool) mgl32.Vec3 {
	if explicit {
		return vec64To32(mgl64.Vec3{rot.Pitch(), rot.Yaw(), headYaw})
	}
	pitch, yaw, hy := entityNetworkRotation(e, rot)
	return vec64To32(mgl64.Vec3{pitch, yaw, hy})
}

// clearThrottledClose drops the throttle entry of an entity that entered
// close range and returns any suppressed delta parts that still need to go.
func (s *Session) clearThrottledClose(h *world.EntityHandle) world.MoveParts {
	s.moveThrottleMu.Lock()
	st, ok := s.moveThrottle[h]
	if ok {
		delete(s.moveThrottle, h)
		if len(s.moveThrottle) == 0 {
			s.hasThrottled.Store(false)
			s.hasPendingThrottled.Store(false)
		}
	}
	s.moveThrottleMu.Unlock()
	if ok && st.pending {
		return st.pendingParts
	}
	return 0
}

// tickMoveThrottle advances the session's movement tick counter and flushes
// the trailing edge of any distant entity whose last movement was suppressed
// and whose interval has since elapsed without a newer update.
func (s *Session) tickMoveThrottle() {
	now := s.moveTick.Add(1)
	if !s.hasPendingThrottled.Load() {
		return
	}
	s.moveThrottleMu.Lock()
	flush := s.moveThrottleFlush[:0]
	pendingLeft := false
	for h, st := range s.moveThrottle {
		if !st.pending {
			if now > st.lastSentTick && now-st.lastSentTick >= st.pendingInterval {
				delete(s.moveThrottle, h)
			}
			continue
		}
		if now > st.lastSentTick && now-st.lastSentTick > st.pendingInterval {
			flush = append(flush, throttledFlushPacket{
				runtimeID: st.pendingRuntime,
				pos:       st.pendingPos,
				rot:       st.pendingRot,
				onGround:  st.pendingOnGround,
			})
			st.pending = false
			st.pendingParts = 0
			st.lastSentTick = now
			st.lastSentPos = st.pendingWorldPos
			st.lastSentOnGround = st.pendingOnGround
			s.moveThrottle[h] = st
		} else {
			pendingLeft = true
		}
	}
	s.hasThrottled.Store(len(s.moveThrottle) > 0)
	s.hasPendingThrottled.Store(pendingLeft)
	s.moveThrottleFlush = flush[:0]
	s.moveThrottleMu.Unlock()

	for _, p := range flush {
		flags := byte(0)
		if p.onGround {
			flags |= mc.MoveFlagOnGround
		}
		s.writePacket(&mc.MoveActorAbsolutePacket{
			EntityRuntimeID: p.runtimeID,
			Position:        p.pos,
			Rotation:        p.rot,
			Flags:           flags,
		})
	}
}

// resetMoveThrottle synchronizes an entity's throttle state after an
// authoritative displacement, teleport, or congestion catch-up so stale
// suppressed state cannot overwrite the authoritative position.
func (s *Session) resetMoveThrottle(h *world.EntityHandle, pos mgl64.Vec3, onGround bool) {
	if !s.hasThrottled.Load() {
		return
	}
	closeRange := s.moveThrottleFactor(pos) <= 1
	now := s.moveTick.Load()
	s.moveThrottleMu.Lock()
	st, ok := s.moveThrottle[h]
	if ok {
		if closeRange {
			delete(s.moveThrottle, h)
			if len(s.moveThrottle) == 0 {
				s.hasThrottled.Store(false)
				s.hasPendingThrottled.Store(false)
			}
		} else {
			s.moveThrottle[h] = moveThrottleState{
				lastSentTick:     now,
				lastSentPos:      pos,
				lastSentOnGround: onGround,
				pendingInterval:  st.pendingInterval,
			}
		}
	}
	s.moveThrottleMu.Unlock()
}

// forgetMoveThrottle removes an entity's throttle state when it is hidden or
// goes out of tracking range.
func (s *Session) forgetMoveThrottle(h *world.EntityHandle) {
	if !s.hasThrottled.Load() {
		return
	}
	s.moveThrottleMu.Lock()
	if _, ok := s.moveThrottle[h]; ok {
		delete(s.moveThrottle, h)
		if len(s.moveThrottle) == 0 {
			s.hasThrottled.Store(false)
			s.hasPendingThrottled.Store(false)
		}
	}
	s.moveThrottleMu.Unlock()
}

// clearMoveThrottle releases all throttle state when the session closes.
func (s *Session) clearMoveThrottle() {
	if !s.hasThrottled.Load() && !s.hasPendingThrottled.Load() {
		return
	}
	s.moveThrottleMu.Lock()
	clear(s.moveThrottle)
	s.moveThrottle = nil
	s.moveThrottleFlush = nil
	s.hasThrottled.Store(false)
	s.hasPendingThrottled.Store(false)
	s.moveThrottleMu.Unlock()
}
