package session

import (
	"bytes"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/wire"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// movingEntity is an entity a test moves.
type movingEntity struct{ h *world.EntityHandle }

func (e movingEntity) Close() error            { return nil }
func (e movingEntity) H() *world.EntityHandle  { return e.h }
func (e movingEntity) Position() mgl64.Vec3    { return mgl64.Vec3{} }
func (e movingEntity) Rotation() cube.Rotation { return cube.Rotation{} }

// TestEntityMoveIsEncodedOnce shows a move to two sessions: each is shown
// all of the first move after the entity appeared, as the client has it
// where it appeared, then both the same packet of the parts that changed,
// encoded once.
func TestEntityMoveIsEncodedOnce(t *testing.T) {
	e := movingEntity{h: entity.NewText("mob", mgl64.Vec3{})}
	a, b := testSession("a"), testSession("b")
	for _, s := range []*Session{a, b} {
		s.freshMoves = map[*world.EntityHandle]struct{}{e.h: {}}
	}
	first := &world.EntityMove{Pos: mgl64.Vec3{1, 64, 1}, OnGround: true, Changed: world.MoveX, Ticks: 3}
	a.ViewEntityMove(e, first)
	b.ViewEntityMove(e, first)
	for _, s := range []*Session{a, b} {
		pk := (<-s.packets).(*v2193.MoveActorDeltaPacket)
		if !pk.PositionX.Set || !pk.PositionY.Set || !pk.PositionZ.Set || !pk.RotationX.Set || !pk.RotationY.Set || !pk.RotationYHead.Set {
			t.Fatalf("first move of a fresh entity: %+v, want every part", pk)
		}
	}
	if first.Shared != nil {
		t.Fatal("moves of fresh entities were shared")
	}
	next := &world.EntityMove{Pos: mgl64.Vec3{1.3, 64, 1}, OnGround: true, Changed: world.MoveX, Ticks: 3}
	a.ViewEntityMove(e, next)
	b.ViewEntityMove(e, next)
	pa, pb := <-a.packets, <-b.packets
	if pa != pb {
		t.Fatal("the sessions were not given the same packet")
	}
	var wa, wb wire.Writer
	pa.Encode(&wa)
	pb.Encode(&wb)
	if !bytes.Equal(wa.B, wb.B) {
		t.Fatal("the shared packet encoded differently")
	}
	var got v2193.MoveActorDeltaPacket
	got.Decode(wire.NewReader(wa.B))
	if !got.PositionX.Set || got.PositionY.Set || got.PositionZ.Set || got.RotationY.Set || got.Ticks != 3 || got.EntityRuntimeID != e.h.RuntimeID() {
		t.Fatalf("move %+v, want x over 3 ticks", got)
	}
	// In the air, the velocity goes with the move.
	jump := &world.EntityMove{Pos: mgl64.Vec3{1.3, 64.4, 1}, Vel: mgl64.Vec3{0, 0.42, 0}, Changed: world.MoveY | world.MoveVelocity}
	a.ViewEntityMove(e, jump)
	if _, ok := (<-a.packets).(*sharedPacket); !ok {
		t.Fatal("the move of the jump is not shared")
	}
	motion := (<-a.packets).(*sharedPacket)
	if pk, ok := motion.Packet.(*v2193.SetActorMotionPacket); !ok || pk.Velocity != [3]float32{0, 0.42, 0} {
		t.Fatalf("motion %+v", motion.Packet)
	}
	// A velocity alone goes without a delta.
	push := &world.EntityMove{Pos: mgl64.Vec3{1.3, 64.4, 1}, Vel: mgl64.Vec3{0.5, 0, 0}, Changed: world.MoveVelocity}
	a.ViewEntityMove(e, push)
	if pk, ok := (<-a.packets).(*sharedPacket); !ok || pk.Packet.(*v2193.SetActorMotionPacket).Velocity != [3]float32{0.5, 0, 0} {
		t.Fatalf("push shown as %+v, want the velocity", pk)
	}
	select {
	case pk := <-a.packets:
		t.Fatalf("a push shown with %T", pk)
	default:
	}
	// A session is not shown the moves of its own player.
	a.ViewEntityMove(movingEntity{h: a.ent}, next)
	select {
	case pk := <-a.packets:
		t.Fatalf("own player shown a move: %T", pk)
	default:
	}
}

// TestAbsoluteMoveIsEncodedOnce shows the move of a player to the sessions
// that view it and to its own: the viewers are given the same packet,
// encoded once, and its own session the move under its own runtime ID,
// unless its client made the move.
func TestAbsoluteMoveIsEncodedOnce(t *testing.T) {
	a, b, own := testSession("a"), testSession("b"), testSession("own")
	p := movingEntity{h: own.ent}
	move := &world.EntityMove{Pos: mgl64.Vec3{1, 64, 1}, Rot: cube.Rotation{90, 10}, OnGround: true, Absolute: true}
	for _, s := range []*Session{a, b, own} {
		s.ViewEntityMove(p, move)
	}
	pa, pb := <-a.packets, <-b.packets
	if pa != pb {
		t.Fatal("the viewers were not given the same packet")
	}
	var w wire.Writer
	pa.Encode(&w)
	var got v2193.MoveActorAbsolutePacket
	got.Decode(wire.NewReader(w.B))
	if got.EntityRuntimeID != own.ent.RuntimeID() || got.Flags != packet.MoveFlagOnGround || got.Position[1] != 64 || got.Rotation[1] != 90 {
		t.Fatalf("viewer shown %+v", got)
	}
	if self := (<-own.packets).(*v2193.MoveActorAbsolutePacket); self.EntityRuntimeID != selfEntityRuntimeID {
		t.Fatalf("own session shown %+v", self)
	}
	own.moving.Store(true)
	own.ViewEntityMove(p, move)
	select {
	case pk := <-own.packets:
		t.Fatalf("a move its client made echoed back: %T", pk)
	default:
	}
}

// countingPacket counts the times it is encoded.
type countingPacket struct {
	proto.Packet
	n *int
}

func (p countingPacket) Encode(w *wire.Writer) { *p.n++; p.Packet.Encode(w) }

func TestSharedPacketEncodesOnce(t *testing.T) {
	n := 0
	p := &sharedPacket{Packet: countingPacket{&v2193.SetActorMotionPacket{EntityRuntimeID: 9}, &n}}
	for range 5 {
		var w wire.Writer
		p.Encode(&w)
	}
	if n != 1 {
		t.Fatalf("encoded %d times", n)
	}
}
