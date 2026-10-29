package session

import (
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/wire"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestSessionPacketBurstPreservesOrder(t *testing.T) {
	c := newQueueTestConn()
	s := (Config{Log: slog.New(slog.DiscardHandler), MaxChunkRadius: 8}).New(c)
	t.Cleanup(s.CloseConnection)
	waitQueueSignal(t, c.started, "packet writer to start")

	// Hold the writer while the world publishes more than the old 256-packet
	// capacity plus its 64-overflow disconnect threshold, as in the client log.
	const burst = 4096
	want := make([]proto.Packet, 0, 2*burst)
	for i := range burst {
		pk := &v2193.AddItemActorPacket{EntityRuntimeID: uint64(i + 2)}
		want = append(want, pk)
		s.writePacket(pk)
	}
	if s.closing.Load() {
		t.Fatal("transient burst disconnected the session")
	}

	close(c.release)
	// New movement must stay behind the already queued spawns as the writer
	// drains the backlog. This also exercises compaction and FIFO handoff.
	for i := range burst {
		pk := &v2193.MoveActorAbsolutePacket{EntityRuntimeID: uint64(i + 2)}
		want = append(want, pk)
		s.writePacket(pk)
	}
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for i := 0; i < len(want); {
		select {
		case pk := <-c.written:
			switch pk.(type) {
			case *v2193.AddItemActorPacket, *v2193.MoveActorAbsolutePacket:
				if pk != want[i] {
					t.Fatalf("packet %d = %v, want %v", i, pk, want[i])
				}
				i++
			}
		case <-timeout.C:
			t.Fatalf("received %d of %d packets", i, len(want))
		}
	}
	if s.closing.Load() {
		t.Fatal("session closed while draining a transient burst")
	}
	s.packetsMu.Lock()
	defer s.packetsMu.Unlock()
	if len(s.packets) != 0 || len(s.pendingPackets) != 0 {
		t.Fatal("drained queue retained packets")
	}
}

func TestSessionPacketBacklogBounded(t *testing.T) {
	c := newQueueTestConn()
	s := (Config{Log: slog.New(slog.DiscardHandler), MaxChunkRadius: 8}).New(c)
	t.Cleanup(s.CloseConnection)
	waitQueueSignal(t, c.started, "packet writer to start")

	for range maxQueuedPackets + 1 {
		s.writePacket(&v2193.SetActorMotionPacket{EntityRuntimeID: 2})
	}
	waitQueueSignal(t, c.closed, "overloaded connection to close")
	s.writePacket(&v2193.TextPacket{Message: "after close"})
	s.packetsMu.Lock()
	defer s.packetsMu.Unlock()
	if len(s.packets) != 0 || len(s.pendingPackets) != 0 {
		t.Fatal("closed session retained queued packets")
	}
}

func TestSessionPacketWriteFailureClosesConnection(t *testing.T) {
	c := newQueueTestConn()
	c.writeErr = io.ErrClosedPipe
	s := (Config{Log: slog.New(slog.DiscardHandler), MaxChunkRadius: 8}).New(c)
	t.Cleanup(s.CloseConnection)
	waitQueueSignal(t, c.started, "packet writer to start")
	close(c.release)
	waitQueueSignal(t, c.closed, "failed writer to close the connection")
	if !s.closing.Load() {
		t.Fatal("failed packet write did not stop the session")
	}
}

func TestSessionPacketQueueConcurrentProducers(t *testing.T) {
	c := newQueueTestConn()
	s := (Config{Log: slog.New(slog.DiscardHandler), MaxChunkRadius: 8}).New(c)
	t.Cleanup(s.CloseConnection)
	waitQueueSignal(t, c.started, "packet writer to start")
	close(c.release)

	const producers, perProducer = 4, 1024
	var wg sync.WaitGroup
	for producer := range producers {
		wg.Go(func() {
			for i := range perProducer {
				s.writePacket(&v2193.TextPacket{Message: strconv.Itoa(producer*perProducer + i)})
			}
		})
	}
	wg.Wait()
	next := make([]int, producers)
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for received := 0; received < producers*perProducer; {
		select {
		case raw := <-c.written:
			pk, ok := raw.(*v2193.TextPacket)
			if !ok {
				continue
			}
			id, err := strconv.Atoi(pk.Message)
			if err != nil || id < 0 || id >= producers*perProducer {
				t.Fatalf("unexpected packet: %v", pk)
			}
			producer, seq := id/perProducer, id%perProducer
			if seq != next[producer] {
				t.Fatalf("producer %d: got sequence %d, want %d", producer, seq, next[producer])
			}
			next[producer]++
			received++
		case <-timeout.C:
			t.Fatalf("received %d of %d concurrent packets", received, producers*perProducer)
		}
	}
}

// TestSessionPacketsHeldDuringBatch checks the packets queued between
// BeginBatch and EndBatch reach the connection only once the batch ends, in
// order after those queued before and before those queued after.
func TestSessionPacketsHeldDuringBatch(t *testing.T) {
	c := newQueueTestConn()
	s := (Config{Log: slog.New(slog.DiscardHandler), MaxChunkRadius: 8}).New(c)
	t.Cleanup(s.CloseConnection)
	close(c.release)

	text := func(i int) *v2193.TextPacket { return &v2193.TextPacket{Message: strconv.Itoa(i)} }
	next := 0
	expect := func(upTo int) {
		t.Helper()
		timeout := time.NewTimer(5 * time.Second)
		defer timeout.Stop()
		for next < upTo {
			select {
			case raw := <-c.written:
				pk, ok := raw.(*v2193.TextPacket)
				if !ok {
					continue
				}
				if pk.Message != strconv.Itoa(next) {
					t.Fatalf("packet %q written, want %d", pk.Message, next)
				}
				next++
			case <-timeout.C:
				t.Fatalf("written %d of %d packets", next, upTo)
			}
		}
	}

	s.writePacket(text(0))
	expect(1)
	s.BeginBatch()
	const held = 600
	for i := 1; i <= held; i++ {
		s.writePacket(text(i))
	}
	select {
	case pk := <-c.written:
		t.Fatalf("packet %v written while held", pk)
	case <-time.After(50 * time.Millisecond):
	}
	s.EndBatch()
	s.writePacket(text(held + 1))
	expect(held + 2)

	// A batch begun twice ends with its second end.
	s.BeginBatch()
	s.BeginBatch()
	s.writePacket(text(held + 2))
	s.EndBatch()
	select {
	case pk := <-c.written:
		t.Fatalf("packet %v written before the outer batch ended", pk)
	case <-time.After(50 * time.Millisecond):
	}
	s.EndBatch()
	expect(held + 3)
}

// TestSessionHeldPacketsBounded checks packets held during a batch count
// towards the backlog that closes a connection that cannot keep up.
func TestSessionHeldPacketsBounded(t *testing.T) {
	c := newQueueTestConn()
	s := (Config{Log: slog.New(slog.DiscardHandler), MaxChunkRadius: 8}).New(c)
	t.Cleanup(s.CloseConnection)

	s.BeginBatch()
	for range maxQueuedPackets + 1 {
		s.writePacket(&v2193.SetActorMotionPacket{EntityRuntimeID: 2})
	}
	waitQueueSignal(t, c.closed, "overloaded connection to close")
	s.EndBatch()
	s.packetsMu.Lock()
	defer s.packetsMu.Unlock()
	if len(s.packets) != 0 || len(s.pendingPackets) != 0 || len(s.heldPackets) != 0 {
		t.Fatal("closed session retained queued packets")
	}
}

func waitQueueSignal(t *testing.T, signal <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for " + what)
	}
}

type queueTestConn struct {
	benchmarkConn
	started, release, closed chan struct{}
	written                  chan proto.Packet
	startOnce, closeOnce     sync.Once
	writeErr                 error
}

func newQueueTestConn() *queueTestConn {
	return &queueTestConn{
		started: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{}),
		written: make(chan proto.Packet, maxQueuedPackets+16),
	}
}

func (c *queueTestConn) WritePacket(pk proto.Packet) error {
	c.startOnce.Do(func() { close(c.started) })
	select {
	case <-c.closed:
		return net.ErrClosed
	case <-c.release:
	}
	if c.writeErr != nil {
		return c.writeErr
	}
	select {
	case <-c.closed:
		return net.ErrClosed
	case c.written <- pk:
		return nil
	}
}

func (c *queueTestConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

var _ Conn = (*queueTestConn)(nil)

// marshalConn encodes each packet written to it, as a real connection does.
type marshalConn struct{ *queueTestConn }

func (c marshalConn) WritePacket(pk proto.Packet) error {
	var w wire.Writer
	pk.Encode(&w)
	return c.queueTestConn.WritePacket(pk)
}

// TestSessionDropsPacketThatDoesNotEncode queues a game rule of a value the
// game has no type for, whose encoder panics, then a valid packet: the
// first is dropped, the second written, and the session goes on.
func TestSessionDropsPacketThatDoesNotEncode(t *testing.T) {
	c := newQueueTestConn()
	s := (Config{Log: slog.New(slog.DiscardHandler), MaxChunkRadius: 8}).New(marshalConn{c})
	t.Cleanup(s.CloseConnection)
	s.writePacket(&v2193.GameRulesChangedPacket{GameRules: []v2193.GameRule{{Name: "rule", Value: struct{}{}}}})
	want := &v2193.TextPacket{TextType: packet.TextTypeRaw, Message: "after"}
	s.writePacket(want)
	close(c.release)
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case pk := <-c.written:
			if pk == want {
				if s.closing.Load() {
					t.Fatal("the session closed over a packet that did not encode")
				}
				return
			}
		case <-timer.C:
			t.Fatal("the packet after the one that did not encode was not written")
		}
	}
}

// TestEmptyMessageShowsAnEmptyLine sends an empty chat message, which
// Bedrock refuses as it is: a space goes instead, as Java shows a line.
func TestEmptyMessageShowsAnEmptyLine(t *testing.T) {
	s := &Session{packets: make(chan proto.Packet, 1), closeBackground: make(chan struct{})}
	s.SendMessage("")
	if text, ok := (<-s.packets).(*v2193.TextPacket); !ok || text.Message != " " {
		t.Fatalf("sent %+v, want a Text of one space", text)
	}
}
