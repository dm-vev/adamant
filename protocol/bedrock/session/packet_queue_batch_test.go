package session

import (
	"errors"
	"io"
	"log/slog"
	"math"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/internal/faultcheck"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

type batchTestConn struct {
	Conn
	writeFn func(pk proto.Packet) error
	written []proto.Packet
	flushes int
}

func (c *batchTestConn) WritePacket(pk proto.Packet) error {
	if c.writeFn != nil {
		if err := c.writeFn(pk); err != nil {
			return err
		}
	}
	c.written = append(c.written, pk)
	return nil
}

func (c *batchTestConn) Flush() error {
	c.flushes++
	return nil
}

func TestWriteBatchResumesAfterEncodePanic(t *testing.T) {
	t.Parallel()
	bad1 := &mc.TextPacket{Message: "panic-1"}
	bad2 := &mc.TextPacket{Message: "panic-2"}
	ok1 := &mc.SetTimePacket{Time: 1}
	ok2 := &mc.SetTimePacket{Time: 2}
	ok3 := &mc.SetTimePacket{Time: 3}

	conn := &batchTestConn{
		writeFn: func(pk proto.Packet) error {
			if pk == bad1 || pk == bad2 {
				panic(faultcheck.Expected{Why: "encode panic"})
			}
			return nil
		},
	}
	s := &Session{
		conf: Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))},
		conn: conn,
	}

	batch := []proto.Packet{bad1, ok1, bad2, ok2, ok3}
	if err := s.writeBatch(batch); err != nil {
		t.Fatalf("writeBatch returned error after recovered panics: %v", err)
	}
	if conn.flushes != 1 {
		t.Fatalf("flushes = %d, want 1", conn.flushes)
	}
	want := []proto.Packet{ok1, ok2, ok3}
	if len(conn.written) != len(want) {
		t.Fatalf("written = %d packets, want %d", len(conn.written), len(want))
	}
	for i := range want {
		if conn.written[i] != want[i] {
			t.Fatalf("written[%d] = %v, want %v", i, conn.written[i], want[i])
		}
	}
}

func TestWriteBatchReturnsErrorWithoutFlush(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("broken pipe")
	bad := &mc.TextPacket{Message: "panic"}
	fail := &mc.SetTimePacket{Time: 2}
	after := &mc.SetTimePacket{Time: 3}

	conn := &batchTestConn{
		writeFn: func(pk proto.Packet) error {
			if pk == bad {
				panic(faultcheck.Expected{Why: "encode panic before write error"})
			}
			if pk == fail {
				return wantErr
			}
			return nil
		},
	}
	s := &Session{
		conf: Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))},
		conn: conn,
	}

	if err := s.writeBatch([]proto.Packet{bad, fail, after}); !errors.Is(err, wantErr) {
		t.Fatalf("writeBatch err = %v, want %v", err, wantErr)
	}
	if conn.flushes != 0 {
		t.Fatalf("flushes = %d, want 0 after write error", conn.flushes)
	}
	if len(conn.written) != 0 {
		t.Fatalf("written = %d packets, want 0", len(conn.written))
	}
}

type inboxTestHandler struct {
	fn func(pk proto.Packet) error
}

func (h inboxTestHandler) Handle(pk proto.Packet, _ *Session, _ *world.Tx, _ Controllable) error {
	return h.fn(pk)
}

func TestHandleInboxPanicAndDeferSemantics(t *testing.T) {
	t.Parallel()
	t.Run("empty", func(t *testing.T) {
		s := &Session{conf: Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}}
		failed, left := s.handleInbox(nil, nil, nil)
		if failed || left {
			t.Fatalf("empty handleInbox = (%v, %v), want (false, false)", failed, left)
		}
	})

	t.Run("panic clears remainder and resets handlingInput", func(t *testing.T) {
		s := &Session{
			conf: Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))},
			handlers: map[uint32]packetHandler{
				mc.IDText: inboxTestHandler{fn: func(proto.Packet) error {
					panic(faultcheck.Expected{Why: "inbox handler panic"})
				}},
			},
		}
		packets := []received{
			{pk: &mc.TextPacket{Message: "boom"}, cost: 10, at: time.Now()},
			{pk: &mc.SetTimePacket{Time: 1}, cost: 20, at: time.Now()},
		}
		failed, left := s.handleInbox(packets, nil, nil)
		if !failed || left {
			t.Fatalf("handleInbox on panic = (%v, %v), want (true, false)", failed, left)
		}
		if s.handlingInput.Load() {
			t.Fatal("handlingInput remained true after handler panic")
		}
		for i, r := range packets {
			if r.pk != nil {
				t.Fatalf("packets[%d] not cleared after panic", i)
			}
		}
	})

	t.Run("deferred requeues packet and tail in order", func(t *testing.T) {
		s := &Session{
			conf: Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))},
			handlers: map[uint32]packetHandler{
				mc.IDText: inboxTestHandler{fn: func(proto.Packet) error {
					return errInputDeferred
				}},
			},
		}
		p1 := &mc.TextPacket{Message: "defer"}
		p2 := &mc.SetTimePacket{Time: 2}
		packets := []received{
			{pk: p1, cost: 11},
			{pk: p2, cost: 22},
		}
		failed, left := s.handleInbox(packets, nil, nil)
		if failed || left {
			t.Fatalf("handleInbox on defer = (%v, %v), want (false, false)", failed, left)
		}
		requeued, _ := s.takeInbox()
		if len(requeued) != 2 || requeued[0].pk != p1 || requeued[1].pk != p2 {
			t.Fatalf("requeued = %+v, want [%v %v]", requeued, p1, p2)
		}
	})
}

func TestBoxFloat32ExactBitPatterns(t *testing.T) {
	t.Parallel()
	values := []float32{0, float32(math.Copysign(0, -1)), 0.25, 0.3125, 0.5, 0.6, 0.98, 1, 1.5, 1.8, 1.9, 2, 3.14}
	for _, v := range values {
		got, ok := boxFloat32(v).(float32)
		if !ok || math.Float32bits(got) != math.Float32bits(v) {
			t.Fatalf("boxFloat32(%v) = %v (bits %#x), want bits %#x", v, got, math.Float32bits(got), math.Float32bits(v))
		}
	}
}
