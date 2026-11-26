package session

import (
	"bytes"
	"math"
	"sync"
	"testing"

	"github.com/dm-vev/adamant/protocol/bedrock/netconn"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/entity"
	"github.com/dm-vev/adamant/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/net/bedrock/wire"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
)

type closeableTestConn struct{ testConn }

func (closeableTestConn) Close() error { return nil }

func setViewerPos(s *Session, pos mgl64.Vec3) {
	p := pos
	s.viewPos.Store(&p)
}

func drainTestPackets(s *Session) []proto.Packet {
	var out []proto.Packet
	for pk := s.testPacket(); pk != nil; pk = s.testPacket() {
		out = append(out, pk)
	}
	return out
}

func decodeDelta(t *testing.T, pk proto.Packet) mc.MoveActorDeltaPacket {
	t.Helper()
	if sh, ok := pk.(*netconn.Shared); ok {
		pk = sh.Packet
	}
	if d, ok := pk.(*mc.MoveActorDeltaPacket); ok {
		return *d
	}
	var w wire.Writer
	pk.Encode(&w)
	var got mc.MoveActorDeltaPacket
	got.Decode(wire.NewReader(w.B))
	return got
}

func unwrapMoveAbsolute(pk proto.Packet) (*mc.MoveActorAbsolutePacket, bool) {
	if sh, ok := pk.(*netconn.Shared); ok {
		pk = sh.Packet
	}
	abs, ok := pk.(*mc.MoveActorAbsolutePacket)
	return abs, ok
}

// TestMoveThrottleCloseEntitiesUnthrottled verifies entities within 32 blocks
// receive every movement update without populating throttle state.
func TestMoveThrottleCloseEntitiesUnthrottled(t *testing.T) {
	t.Parallel()
	s := testSession("close")
	setViewerPos(s, mgl64.Vec3{0, 64, 0})

	e1 := movingEntity{h: entity.NewText("near1", mgl64.Vec3{10, 64, 0})}
	e2 := movingEntity{h: entity.NewText("boundary", mgl64.Vec3{32, 64, 0})}

	const ticks = 20
	for i := range ticks {
		s.tickMoveThrottle()
		pos1 := mgl64.Vec3{10 + float64(i)*0.1, 64, 0}
		s.ViewEntityMovement(e1, pos1, cube.Rotation{float64(i), 0}, true)

		// Exact 32-block boundary stays unthrottled.
		pos2 := mgl64.Vec3{32, 64, 0}
		s.ViewEntityMove(e2, &world.EntityMove{
			Pos: pos2, Rot: cube.Rotation{float64(i), 0}, OnGround: true, Absolute: true,
		})
	}

	pks := drainTestPackets(s)
	if len(pks) != 2*ticks {
		t.Fatalf("got %d packets for close entities over %d ticks, want %d", len(pks), ticks, 2*ticks)
	}
	if s.hasThrottled.Load() || s.hasPendingThrottled.Load() {
		t.Fatal("close entities populated throttle state")
	}
}

// TestMoveThrottleDistanceTiers verifies packet rates across distance tiers
// for both 1-tick moves (players/ViewEntityMovement) and 3-tick mob deltas,
// including 3D vertical distance.
func TestMoveThrottleDistanceTiers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		startPos mgl64.Vec3
		step     mgl64.Vec3
		wantSent int // out of 40 ticks (leading edge on tick 0, then every factor ticks)
	}{
		{name: "near_20", startPos: mgl64.Vec3{20, 64, 0}, step: mgl64.Vec3{0, 0, 0.05}, wantSent: 40},
		{name: "mid_48", startPos: mgl64.Vec3{48, 64, 0}, step: mgl64.Vec3{0, 0, 0.05}, wantSent: 20},
		{name: "mid_vertical_48", startPos: mgl64.Vec3{0, 16, 0}, step: mgl64.Vec3{0.05, 0, 0}, wantSent: 20},
		{name: "far_96", startPos: mgl64.Vec3{96, 64, 0}, step: mgl64.Vec3{0, 0, 0.05}, wantSent: 10},
		{name: "very_far_160", startPos: mgl64.Vec3{160, 64, 0}, step: mgl64.Vec3{0, 0, 0.05}, wantSent: 5},
	}

	for _, tc := range cases {
		t.Run(tc.name+"_absolute", func(t *testing.T) {
			t.Parallel()
			s := testSession(tc.name)
			setViewerPos(s, mgl64.Vec3{0, 64, 0})
			e := movingEntity{h: entity.NewText(tc.name, tc.startPos)}

			for i := range 40 {
				s.tickMoveThrottle()
				pos := tc.startPos.Add(tc.step.Mul(float64(i)))
				s.ViewEntityMove(e, &world.EntityMove{
					Pos: pos, Rot: cube.Rotation{float64(i), 0}, OnGround: true, Absolute: true,
				})
			}
			got := len(drainTestPackets(s))
			if got != tc.wantSent {
				t.Fatalf("ViewEntityMove(Absolute) at %s: sent %d packets over 40 ticks, want %d", tc.name, got, tc.wantSent)
			}
		})

		t.Run(tc.name+"_movement", func(t *testing.T) {
			t.Parallel()
			s := testSession(tc.name + "-mv")
			setViewerPos(s, mgl64.Vec3{0, 64, 0})
			e := movingEntity{h: entity.NewText(tc.name, tc.startPos)}

			for i := range 40 {
				s.tickMoveThrottle()
				pos := tc.startPos.Add(tc.step.Mul(float64(i)))
				s.ViewEntityMovement(e, pos, cube.Rotation{float64(i), 0}, true)
			}
			got := len(drainTestPackets(s))
			if got != tc.wantSent {
				t.Fatalf("ViewEntityMovement at %s: sent %d packets over 40 ticks, want %d", tc.name, got, tc.wantSent)
			}
		})
	}

	// Verify 3-tick mob delta cadence (MoveInterval=3) is also throttled by factor.
	t.Run("mob_delta_tiers", func(t *testing.T) {
		t.Parallel()
		s := testSession("mob-delta")
		setViewerPos(s, mgl64.Vec3{0, 64, 0})
		midMob := movingEntity{h: entity.NewText("mid-mob", mgl64.Vec3{48, 64, 0})}
		farMob := movingEntity{h: entity.NewText("far-mob", mgl64.Vec3{96, 64, 0})}

		var midSent, farSent int
		// 24 mob updates spaced 3 ticks apart = 72 world ticks.
		for tick := 1; tick <= 72; tick++ {
			s.tickMoveThrottle()
			if tick%world.MoveInterval == 0 {
				step := float64(tick/world.MoveInterval) * 0.05
				s.ViewEntityMove(midMob, &world.EntityMove{
					Pos: mgl64.Vec3{48, 64, step}, OnGround: true, Changed: world.MoveZ, Ticks: world.MoveInterval,
				})
				s.ViewEntityMove(farMob, &world.EntityMove{
					Pos: mgl64.Vec3{96, 64, step}, OnGround: true, Changed: world.MoveZ, Ticks: world.MoveInterval,
				})
			}
			for _, pk := range drainTestPackets(s) {
				d := decodeDelta(t, pk)
				switch d.EntityRuntimeID {
				case midMob.h.RuntimeID():
					midSent++
				case farMob.h.RuntimeID():
					farSent++
				}
			}
		}
		if midSent != 12 {
			t.Fatalf("mid-range mob sent %d delta packets out of 24 updates, want 12 (50%%)", midSent)
		}
		if farSent != 6 {
			t.Fatalf("far-range mob sent %d delta packets out of 24 updates, want 6 (75%%)", farSent)
		}
	})
}

// TestMoveThrottleNeverThrottlesSelfOrAuthoritative verifies selfEntityRuntimeID,
// ViewEntityDisplacement, and ViewEntityTeleport are never throttled and reset
// pending throttle state.
func TestMoveThrottleNeverThrottlesSelfOrAuthoritative(t *testing.T) {
	t.Parallel()
	s := testSession("auth")
	setViewerPos(s, mgl64.Vec3{0, 64, 0})

	// Own entity far from viewPos must never be throttled.
	self := movingEntity{h: s.ent}
	for i := range 10 {
		s.tickMoveThrottle()
		s.ViewEntityMovement(self, mgl64.Vec3{200 + float64(i)*0.1, 64, 0}, cube.Rotation{}, true)
	}
	if got := len(drainTestPackets(s)); got != 10 {
		t.Fatalf("self entity sent %d packets, want 10 (never throttled)", got)
	}

	// Distant entity with a suppressed move, followed by ViewEntityDisplacement.
	far := movingEntity{h: entity.NewText("far", mgl64.Vec3{160, 64, 0})}
	s.tickMoveThrottle()
	s.ViewEntityMovement(far, mgl64.Vec3{160, 64, 0}, cube.Rotation{}, true) // leading edge sent
	if len(drainTestPackets(s)) != 1 {
		t.Fatal("leading edge not sent")
	}
	s.tickMoveThrottle()
	s.ViewEntityMovement(far, mgl64.Vec3{160.2, 64, 0}, cube.Rotation{}, true) // suppressed (pending!)
	if len(drainTestPackets(s)) != 0 {
		t.Fatal("second tick at 160 blocks should have been suppressed")
	}

	// Displacement must go out immediately AND clear pending state.
	dispPos := mgl64.Vec3{170, 64, 0}
	s.ViewEntityDisplacement(far, dispPos, cube.Rotation{90, 0}, true)
	pks := drainTestPackets(s)
	if len(pks) != 1 {
		t.Fatalf("displacement sent %d packets, want 1", len(pks))
	}
	abs, ok := unwrapMoveAbsolute(pks[0])
	if !ok || abs.Flags&mc.MoveFlagTeleport == 0 || abs.Position[0] != 170 {
		t.Fatalf("displacement packet = %+v, want teleport to x=170", pks[0])
	}

	// Repeated displacements must never be throttled.
	for i := range 5 {
		s.ViewEntityDisplacement(far, mgl64.Vec3{170 + float64(i)*0.1, 64, 0}, cube.Rotation{}, true)
	}
	if got := len(drainTestPackets(s)); got != 5 {
		t.Fatalf("repeated displacements sent %d packets, want 5", got)
	}

	// Advance many ticks: stale pre-displacement suppressed position (160.2) must NEVER flush!
	for range 20 {
		s.tickMoveThrottle()
	}
	if pks := drainTestPackets(s); len(pks) != 0 {
		t.Fatalf("stale suppressed move flushed after displacement: %+v", pks)
	}

	// Same check for ViewEntityTeleport.
	s.tickMoveThrottle()
	s.ViewEntityMovement(far, mgl64.Vec3{170.5, 64, 0}, cube.Rotation{}, true) // suppressed (just reset)
	s.ViewEntityTeleport(far, mgl64.Vec3{180, 64, 0})
	if len(drainTestPackets(s)) < 2 {
		t.Fatal("ViewEntityTeleport did not emit packets")
	}
	for range 20 {
		s.tickMoveThrottle()
	}
	if pks := drainTestPackets(s); len(pks) != 0 {
		t.Fatalf("stale suppressed move flushed after teleport: %+v", pks)
	}
}

// TestMoveThrottleBypassesOnGroundAndLargeJump verifies that onGround changes
// and jumps > 4 blocks bypass throttling immediately.
func TestMoveThrottleBypassesOnGroundAndLargeJump(t *testing.T) {
	t.Parallel()
	s := testSession("bypass")
	setViewerPos(s, mgl64.Vec3{0, 64, 0})
	far := movingEntity{h: entity.NewText("far", mgl64.Vec3{160, 64, 0})}

	s.tickMoveThrottle()
	s.ViewEntityMovement(far, mgl64.Vec3{160, 64, 0}, cube.Rotation{}, true) // leading edge
	drainTestPackets(s)

	// Next tick: small move, same onGround -> suppressed.
	s.tickMoveThrottle()
	s.ViewEntityMovement(far, mgl64.Vec3{160.2, 64, 0}, cube.Rotation{}, true)
	if len(drainTestPackets(s)) != 0 {
		t.Fatal("small move on tick 2 should be suppressed")
	}

	// Next tick: onGround flips true -> false -> MUST send immediately!
	s.tickMoveThrottle()
	s.ViewEntityMovement(far, mgl64.Vec3{160.3, 64.4, 0}, cube.Rotation{}, false)
	pks := drainTestPackets(s)
	if len(pks) != 1 {
		t.Fatalf("onGround change sent %d packets, want 1", len(pks))
	}
	if abs, ok := unwrapMoveAbsolute(pks[0]); !ok || abs.Flags&mc.MoveFlagOnGround != 0 {
		t.Fatalf("onGround packet = %+v, want OnGround=false", pks[0])
	}

	// Next tick: jump > 4 blocks (5 blocks) -> MUST send immediately!
	s.tickMoveThrottle()
	s.ViewEntityMovement(far, mgl64.Vec3{165.5, 64.4, 0}, cube.Rotation{}, false)
	pks = drainTestPackets(s)
	if len(pks) != 1 {
		t.Fatalf("large jump sent %d packets, want 1", len(pks))
	}
	if abs, ok := unwrapMoveAbsolute(pks[0]); !ok || math.Abs(float64(abs.Position[0])-165.5) > 0.01 {
		t.Fatalf("large jump packet = %+v, want x=165.5", pks[0])
	}
}

// TestMoveThrottleLeadingAndTrailingEdge verifies that a distant entity that
// moves briefly and stops always delivers its exact final resting position
// via the trailing-edge flush, with no duplicate flushes afterwards.
func TestMoveThrottleLeadingAndTrailingEdge(t *testing.T) {
	t.Parallel()
	s := testSession("edge")
	setViewerPos(s, mgl64.Vec3{0, 64, 0})
	far := movingEntity{h: entity.NewText("far", mgl64.Vec3{100, 64, 0})}

	// Tick 1: leading edge sends immediately.
	s.tickMoveThrottle()
	s.ViewEntityMove(far, &world.EntityMove{
		Pos: mgl64.Vec3{100, 64, 0}, Rot: cube.Rotation{10, 5}, HeadYaw: 15, OnGround: true,
		Changed: world.MoveX | world.MoveYaw | world.MoveHeadYaw, Ticks: 1,
	})
	if len(drainTestPackets(s)) != 1 {
		t.Fatal("leading edge was not sent immediately")
	}

	// Tick 2: suppressed, then entity STOPS moving permanently.
	finalPos := mgl64.Vec3{100.75, 64.25, 1.5}
	s.tickMoveThrottle()
	s.ViewEntityMove(far, &world.EntityMove{
		Pos: finalPos, Rot: cube.Rotation{45, 12}, HeadYaw: 50, OnGround: true,
		Changed: world.MovePosition | world.MoveRotation, Ticks: 1,
	})
	if len(drainTestPackets(s)) != 0 {
		t.Fatal("tick 2 at 100 blocks should have been suppressed")
	}

	// Advance session ticks without any more entity moves.
	var flushed []*mc.MoveActorAbsolutePacket
	for range 10 {
		s.tickMoveThrottle()
		for _, pk := range drainTestPackets(s) {
			if abs, ok := unwrapMoveAbsolute(pk); ok {
				flushed = append(flushed, abs)
			}
		}
	}
	if len(flushed) != 1 {
		t.Fatalf("trailing-edge flush count = %d, want 1", len(flushed))
	}
	wantPos := mgl32.Vec3{100.75, 64.25, 1.5}
	wantRot := mgl32.Vec3{12, 45, 50}
	if flushed[0].Position != wantPos || flushed[0].Rotation != wantRot || flushed[0].Flags != mc.MoveFlagOnGround {
		t.Fatalf("flushed = %+v, want pos=%v rot=%v onGround=true", flushed[0], wantPos, wantRot)
	}
}

// TestMoveThrottleAccumulatesSkippedDeltaParts verifies that axes changed
// during suppressed MoveActorDelta ticks are not lost when a subsequent tick
// only changes a different axis (or when the entity enters close range).
func TestMoveThrottleAccumulatesSkippedDeltaParts(t *testing.T) {
	t.Parallel()
	s := testSession("delta-parts")
	setViewerPos(s, mgl64.Vec3{0, 64, 0})
	e := movingEntity{h: entity.NewText("mob", mgl64.Vec3{48, 64, 0})}

	// Tick 1 (leading edge): MoveX sent.
	s.tickMoveThrottle()
	s.ViewEntityMove(e, &world.EntityMove{
		Pos: mgl64.Vec3{48, 64, 0}, OnGround: true, Changed: world.MoveX, Ticks: 1,
	})
	drainTestPackets(s)

	// Tick 2 (suppressed at 48 blocks, interval 2): MoveY | MoveYaw skipped!
	s.tickMoveThrottle()
	s.ViewEntityMove(e, &world.EntityMove{
		Pos: mgl64.Vec3{48, 64.5, 0}, Rot: cube.Rotation{90, 0}, OnGround: true,
		Changed: world.MoveY | world.MoveYaw, Ticks: 1,
	})
	if len(drainTestPackets(s)) != 0 {
		t.Fatal("tick 2 should have been suppressed")
	}

	// Tick 3 (interval reached): only MoveZ changed on tick 3, but packet MUST
	// also include MoveY and MoveYaw accumulated from tick 2!
	s.tickMoveThrottle()
	s.ViewEntityMove(e, &world.EntityMove{
		Pos: mgl64.Vec3{48, 64.5, 1.25}, Rot: cube.Rotation{90, 0}, OnGround: true,
		Changed: world.MoveZ, Ticks: 1,
	})
	pks := drainTestPackets(s)
	if len(pks) != 1 {
		t.Fatalf("tick 3 sent %d packets, want 1", len(pks))
	}
	got := decodeDelta(t, pks[0])
	if !got.PositionY.Set || got.PositionY.Val != 64.5 ||
		!got.PositionZ.Set || got.PositionZ.Val != 1.25 ||
		!got.RotationY.Set || got.RotationY.Val != 90 {
		t.Fatalf("accumulated delta = %+v, want Y=64.5 Z=1.25 Yaw=90", got)
	}

	// Tick 4 (suppressed): MoveX skipped, then entity steps into close range (<=32) with MoveZ only.
	s.tickMoveThrottle()
	s.ViewEntityMove(e, &world.EntityMove{
		Pos: mgl64.Vec3{47, 64.5, 1.25}, Rot: cube.Rotation{90, 0}, OnGround: true,
		Changed: world.MoveX, Ticks: 1,
	})
	if len(drainTestPackets(s)) != 0 {
		t.Fatal("tick 4 should have been suppressed")
	}

	// Move viewer close so entity enters <= 32 blocks on tick 5.
	setViewerPos(s, mgl64.Vec3{30, 64, 0})
	s.tickMoveThrottle()
	s.ViewEntityMove(e, &world.EntityMove{
		Pos: mgl64.Vec3{47, 64.5, 1.5}, Rot: cube.Rotation{90, 0}, OnGround: true,
		Changed: world.MoveZ, Ticks: 1,
	})
	pks = drainTestPackets(s)
	if len(pks) != 1 {
		t.Fatalf("close transition sent %d packets, want 1", len(pks))
	}
	got = decodeDelta(t, pks[0])
	if !got.PositionX.Set || got.PositionX.Val != 47 || !got.PositionZ.Set || got.PositionZ.Val != 1.5 {
		t.Fatalf("close transition delta = %+v, want X=47 and Z=1.5", got)
	}
	if s.hasThrottled.Load() {
		t.Fatal("throttle entry was not cleared upon entering close range")
	}
}

// TestMoveThrottleCleanupOnHideAndClose verifies HideEntity, hideFar, and
// Close remove all throttle state with zero memory leak and no post-hide flush.
func TestMoveThrottleCleanupOnHideAndClose(t *testing.T) {
	t.Parallel()
	s := testSession("cleanup")
	setViewerPos(s, mgl64.Vec3{0, 64, 0})

	e1 := movingEntity{h: entity.NewText("e1", mgl64.Vec3{100, 64, 0})}
	e2 := movingEntity{h: entity.NewText("e2", mgl64.Vec3{100, 64, 10})}
	e3 := movingEntity{h: entity.NewText("e3", mgl64.Vec3{100, 64, 20})}

	s.entityMutex.Lock()
	s.entityRuntimeIDs[e1.h] = e1.h.RuntimeID()
	s.entities[e1.h.RuntimeID()] = e1.h
	s.entityRuntimeIDs[e2.h] = e2.h.RuntimeID()
	s.entities[e2.h.RuntimeID()] = e2.h
	s.entityRuntimeIDs[e3.h] = e3.h.RuntimeID()
	s.entities[e3.h.RuntimeID()] = e3.h
	s.entityMutex.Unlock()

	// Create pending suppressed state for all three.
	positions := []mgl64.Vec3{{100, 64, 0}, {100, 64, 10}, {100, 64, 20}}
	ents := []movingEntity{e1, e2, e3}
	s.tickMoveThrottle()
	for i, e := range ents {
		s.ViewEntityMovement(e, positions[i], cube.Rotation{}, true)
	}
	drainTestPackets(s)
	s.tickMoveThrottle()
	for i, e := range ents {
		s.ViewEntityMovement(e, positions[i].Add(mgl64.Vec3{0.2, 0, 0}), cube.Rotation{}, true)
	}
	drainTestPackets(s)

	// HideEntity drops e1 immediately.
	s.HideEntity(e1)
	s.moveThrottleMu.Lock()
	_, ok1 := s.moveThrottle[e1.h]
	s.moveThrottleMu.Unlock()
	if ok1 {
		t.Fatal("HideEntity leaked moveThrottle entry")
	}

	// hideFar drops e2 immediately.
	s.hideFar(e2)
	s.moveThrottleMu.Lock()
	_, ok2 := s.moveThrottle[e2.h]
	s.moveThrottleMu.Unlock()
	if ok2 {
		t.Fatal("hideFar leaked moveThrottle entry")
	}
	drainTestPackets(s)

	// Advancing ticks must flush ONLY e3, never hidden e1 or e2.
	for range 10 {
		s.tickMoveThrottle()
	}
	for _, pk := range drainTestPackets(s) {
		if abs, ok := unwrapMoveAbsolute(pk); ok {
			if abs.EntityRuntimeID == e1.h.RuntimeID() || abs.EntityRuntimeID == e2.h.RuntimeID() {
				t.Fatalf("hidden entity %d received trailing flush", abs.EntityRuntimeID)
			}
		}
	}

	// Session.Close clears everything.
	s.conn = closeableTestConn{s.conn.(testConn)}
	s.Close(nil)
	s.moveThrottleMu.Lock()
	n := len(s.moveThrottle)
	s.moveThrottleMu.Unlock()
	if n != 0 || s.hasThrottled.Load() || s.hasPendingThrottled.Load() {
		t.Fatalf("Close left throttle state: len=%d hasThrottled=%v hasPending=%v", n, s.hasThrottled.Load(), s.hasPendingThrottled.Load())
	}
}

// TestMoveThrottleConcurrentRace exercises concurrent region movement calls,
// displacements, hides, and session ticks under -race.
func TestMoveThrottleConcurrentRace(t *testing.T) {
	t.Parallel()
	s := testSession("race")
	s.conn = closeableTestConn{s.conn.(testConn)}
	setViewerPos(s, mgl64.Vec3{0, 64, 0})

	const numEntities = 32
	entities := make([]movingEntity, numEntities)
	for i := range numEntities {
		entities[i] = movingEntity{h: entity.NewText("e", mgl64.Vec3{float64(20 + i*6), 64, 0})}
	}

	var wg sync.WaitGroup
	// Concurrent region workers moving different entities on the same session.
	for worker := range 4 {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for tick := range 60 {
				for i := w; i < numEntities; i += 4 {
					e := entities[i]
					pos := mgl64.Vec3{float64(20 + i*6), 64, float64(tick) * 0.1}
					switch tick % 5 {
					case 0:
						s.ViewEntityMove(e, &world.EntityMove{
							Pos: pos, OnGround: true, Changed: world.MoveZ, Ticks: 1,
						})
					case 1:
						s.ViewEntityMove(e, &world.EntityMove{
							Pos: pos, OnGround: true, Absolute: true,
						})
					case 2:
						s.ViewEntityMovement(e, pos, cube.Rotation{}, true)
					case 3:
						s.ViewEntityDisplacement(e, pos, cube.Rotation{}, true)
					case 4:
						if tick == 59 {
							s.HideEntity(e)
						}
					}
				}
			}
		}(worker)
	}
	// Concurrent viewer tick goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for tick := range 60 {
			setViewerPos(s, mgl64.Vec3{float64(tick%3) * 0.1, 64, 0})
			s.tickMoveThrottle()
			drainTestPackets(s)
		}
	}()
	wg.Wait()
	s.Close(nil)
}

// TestMoveThrottlePacketReduction measures packet savings in a realistic town
// scenario with 50 moving players (up to view radius 512 blocks) and 150
// moving mobs (up to tracking range 128 blocks) over 100 ticks (5 seconds).
func TestMoveThrottlePacketReduction(t *testing.T) {
	t.Parallel()
	s := testSession("town-reduction")
	setViewerPos(s, mgl64.Vec3{0, 64, 0})

	// 50 players spread across radii 16..300 blocks (uniformly by area in a town).
	const numPlayers = 50
	players := make([]movingEntity, numPlayers)
	playerPos := make([]mgl64.Vec3, numPlayers)
	for i := range numPlayers {
		// Area-weighted radial distribution from 16 to 280 blocks.
		r := 16.0 + 264.0*math.Sqrt(float64(i+1)/float64(numPlayers))
		ang := float64(i) * 2.399963229728653 // golden angle
		p := mgl64.Vec3{r * math.Cos(ang), 64, r * math.Sin(ang)}
		playerPos[i] = p
		players[i] = movingEntity{h: entity.NewText("player", p)}
	}

	// 150 mobs spread across tracking radius 10..128 blocks (area-weighted).
	const numMobs = 150
	mobs := make([]movingEntity, numMobs)
	mobPos := make([]mgl64.Vec3, numMobs)
	for i := range numMobs {
		r := 10.0 + 118.0*math.Sqrt(float64(i+1)/float64(numMobs))
		ang := float64(i) * 2.399963229728653
		p := mgl64.Vec3{r * math.Cos(ang), 64, r * math.Sin(ang)}
		mobPos[i] = p
		mobs[i] = movingEntity{h: entity.NewText("mob", p)}
	}

	var rawAbsolute, rawDelta int
	var sentAbsolute, sentDelta int
	const totalTicks = 100

	for tick := 1; tick <= totalTicks; tick++ {
		s.tickMoveThrottle()
		for i, e := range players {
			rawAbsolute++
			pos := playerPos[i].Add(mgl64.Vec3{0.15 * float64(tick%20), 0, 0})
			s.ViewEntityMove(e, &world.EntityMove{
				Pos: pos, OnGround: true, Absolute: true,
			})
		}
		if tick%world.MoveInterval == 0 {
			for i, e := range mobs {
				rawDelta++
				pos := mobPos[i].Add(mgl64.Vec3{0, 0, 0.1 * float64((tick/world.MoveInterval)%10)})
				s.ViewEntityMove(e, &world.EntityMove{
					Pos: pos, OnGround: true, Changed: world.MoveZ, Ticks: world.MoveInterval,
				})
			}
		}
		for _, pk := range drainTestPackets(s) {
			if sh, ok := pk.(*netconn.Shared); ok {
				pk = sh.Packet
			}
			switch pk.(type) {
			case *mc.MoveActorAbsolutePacket:
				sentAbsolute++
			case *mc.MoveActorDeltaPacket:
				sentDelta++
			}
		}
	}

	rawTotal := rawAbsolute + rawDelta
	sentTotal := sentAbsolute + sentDelta
	absSaved := 100.0 * float64(rawAbsolute-sentAbsolute) / float64(rawAbsolute)
	deltaSaved := 100.0 * float64(rawDelta-sentDelta) / float64(rawDelta)
	totalSaved := 100.0 * float64(rawTotal-sentTotal) / float64(rawTotal)

	t.Logf("Town 50 players + 150 mobs over %d ticks: MoveActorAbsolute %d->%d (-%.1f%%), MoveActorDelta %d->%d (-%.1f%%), Total %d->%d (-%.1f%%)",
		totalTicks, rawAbsolute, sentAbsolute, absSaved, rawDelta, sentDelta, deltaSaved, rawTotal, sentTotal, totalSaved)

	if absSaved < 60.0 {
		t.Fatalf("expected >= 60%% MoveActorAbsolute reduction, got %.1f%%", absSaved)
	}
	if deltaSaved < 55.0 {
		t.Fatalf("expected >= 55%% MoveActorDelta reduction, got %.1f%%", deltaSaved)
	}
}

// BenchmarkMoveThrottle verifies zero heap allocations on the close fast path
// and suppressed distant moves.
func BenchmarkMoveThrottle(b *testing.B) {
	b.Run("close_fast_path", func(b *testing.B) {
		s := testSession("bench-close")
		setViewerPos(s, mgl64.Vec3{0, 64, 0})
		e := movingEntity{h: entity.NewText("near", mgl64.Vec3{10, 64, 0})}
		pos := mgl64.Vec3{10, 64, 0}
		rot := cube.Rotation{}

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			s.throttleMove(e.h.RuntimeID(), e, pos, rot, 0, false, true, world.MoveX, 1)
		}
	})

	b.Run("distant_suppressed", func(b *testing.B) {
		s := testSession("bench-far")
		setViewerPos(s, mgl64.Vec3{0, 64, 0})
		e := movingEntity{h: entity.NewText("far", mgl64.Vec3{160, 64, 0})}
		pos := mgl64.Vec3{160, 64, 0}
		rot := cube.Rotation{}
		// Prime leading edge.
		s.throttleMove(e.h.RuntimeID(), e, pos, rot, 0, false, true, world.MoveX, 1)

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			s.throttleMove(e.h.RuntimeID(), e, pos, rot, 0, false, true, world.MoveX, 1)
		}
	})
}

// TestSharedMoveStillReusedAcrossCloseAndDueViewers ensures distance throttling
// preserves shared wire encoding between close and due distant viewers.
func TestSharedMoveStillReusedAcrossCloseAndDueViewers(t *testing.T) {
	t.Parallel()
	near, far := testSession("near"), testSession("far")
	setViewerPos(near, mgl64.Vec3{0, 64, 0})
	setViewerPos(far, mgl64.Vec3{100, 64, 0})
	e := movingEntity{h: entity.NewText("mob", mgl64.Vec3{5, 64, 0})}

	move := &world.EntityMove{Pos: mgl64.Vec3{5.5, 64, 0}, OnGround: true, Changed: world.MoveX, Ticks: 3}
	near.ViewEntityMove(e, move)
	far.ViewEntityMove(e, move) // leading edge: due immediately, no skipped parts

	pn, pf := near.testPacket(), far.testPacket()
	if pn != pf {
		t.Fatal("near and due far viewers did not share the same encoded packet")
	}
	var wn, wf wire.Writer
	pn.Encode(&wn)
	pf.Encode(&wf)
	if !bytes.Equal(wn.B, wf.B) {
		t.Fatal("shared packets encoded differently")
	}
}
