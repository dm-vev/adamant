package session

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
)

// bufferedConn is a connection that holds as much for its client as a test
// says.
type bufferedConn struct {
	testConn
	buffered *atomic.Int64
}

func (c bufferedConn) Buffered() int { return int(c.buffered.Load()) }

// TestCongestedClientGetsFreshState congests a connection: the moves of
// another entity are kept back and sounds dropped rather than queued, the
// moves of the player itself still go, and once the client caught up the
// entity is shown where it is then, once.
func TestCongestedClientGetsFreshState(t *testing.T) {
	w := world.Config{Entities: entity.DefaultRegistry, Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	s := testSession("congested")
	var buffered atomic.Int64
	s.conn = bufferedConn{testConn: s.conn.(testConn), buffered: &buffered}
	handle := entity.NewText("mob", mgl64.Vec3{1, 64, 1})
	if err := w.Do(func(tx *world.Tx) {
		mob := tx.AddEntity(handle)
		s.entityRuntimeIDs[handle] = handle.RuntimeID()

		s.updateCongestion(tx)
		move := &world.EntityMove{Pos: mgl64.Vec3{2, 64, 1}, Changed: world.MoveX, Ticks: 1}
		s.ViewEntityMove(mob, move)
		if pk := s.testPacket(); pk == nil {
			t.Fatal("a move to a client keeping up was not shown")
		}

		buffered.Store(congestedAbove + 1)
		s.updateCongestion(tx)
		for i := range 10 {
			s.ViewEntityMove(mob, &world.EntityMove{Pos: mgl64.Vec3{3 + float64(i), 64, 1}, Changed: world.MoveX, Ticks: 1})
			s.ViewEntityVelocity(mob, mgl64.Vec3{0.1, 0, 0})
		}
		s.ViewSound(mgl64.Vec3{}, nil)
		s.ViewParticle(mgl64.Vec3{}, nil)
		if pk := s.testPacket(); pk != nil {
			t.Fatalf("%T queued for a congested client", packetOf(pk))
		}
		// The player of the session is shown its own moves still.
		s.ViewEntityMove(movingEntity{h: s.ent}, &world.EntityMove{Pos: mgl64.Vec3{5, 64, 5}, Absolute: true})
		if _, ok := (s.testPacket()).(*v2193.MoveActorAbsolutePacket); !ok {
			t.Fatal("a move of the player itself was kept back")
		}

		// Short of clearing, the session stays congested.
		buffered.Store(clearBelow + 1)
		s.updateCongestion(tx)
		if pk := s.testPacket(); pk != nil {
			t.Fatalf("%T shown before the client caught up", packetOf(pk))
		}
		buffered.Store(0)
		s.updateCongestion(tx)
		abs, ok := (s.testPacket()).(*v2193.MoveActorAbsolutePacket)
		if !ok || abs.EntityRuntimeID != handle.RuntimeID() || abs.Position[0] != float32(mob.Position()[0]) {
			t.Fatalf("shown %+v once caught up, want the entity where it is", abs)
		}
		for pk := s.testPacket(); pk != nil; pk = s.testPacket() {
			if _, ok := pk.(*v2193.MoveActorAbsolutePacket); ok {
				t.Fatal("the entity was shown twice")
			}
		}
		s.updateCongestion(tx)
		if pk := s.testPacket(); pk != nil {
			t.Fatalf("%T shown again with nothing kept back", packetOf(pk))
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
