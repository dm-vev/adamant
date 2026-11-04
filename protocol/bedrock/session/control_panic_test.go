package session

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/world"
)

// panicBufferConn is the connection of a test session whose Buffered
// panics, as that of a connection that broke might, and which records
// that it was closed.
type panicBufferConn struct {
	testConn
	closed *atomic.Bool
}

func (panicBufferConn) Buffered() int  { panic("test: buffered") }
func (c panicBufferConn) Close() error { c.closed.Store(true); return nil }

// closingControllable is the Controllable of a test, counting its closes.
// Its other methods are not to be called.
type closingControllable struct {
	Controllable
	closes *atomic.Int32
}

func (c closingControllable) Close() error { c.closes.Add(1); return nil }

func TestSessionControlPanicClosesTheSession(t *testing.T) {
	closed, closes := new(atomic.Bool), new(atomic.Int32)
	s := testSession("Alice")
	s.conn = panicBufferConn{testConn: s.conn.(testConn), closed: closed}
	// The commands were sent: Control goes on to the congestion of the
	// connection, which panics.
	s.commandState.ready = true
	// A world of its own owner: the session leaves it on a goroutine of
	// its own once closed.
	w := world.Config{Provider: world.NopProvider{}, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })

	var recovered any
	<-w.Exec(func(tx *world.Tx) {
		// The player of the session is in the world, so that it leaves it
		// once the session closed.
		tx.AddEntity(s.ent)
		defer func() { recovered = recover() }()
		s.Control(tx, closingControllable{closes: closes})
	})
	if recovered != nil {
		t.Fatalf("Control panicked: %v", recovered)
	}
	if !closed.Load() || closes.Load() != 1 {
		t.Fatalf("connection closed %v, player closed %d times: want the session closed", closed.Load(), closes.Load())
	}
	// The world lives.
	ran := make(chan struct{})
	w.Exec(func(*world.Tx) { close(ran) })
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("the world does not run transactions after the panic")
	}
}
