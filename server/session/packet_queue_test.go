package session

import (
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

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
	want := make([]packet.Packet, 0, 2*burst)
	for i := range burst {
		pk := &packet.AddItemActor{EntityRuntimeID: uint64(i + 2)}
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
		pk := &packet.MoveActorAbsolute{EntityRuntimeID: uint64(i + 2)}
		want = append(want, pk)
		s.writePacket(pk)
	}
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for i := 0; i < len(want); {
		select {
		case pk := <-c.written:
			switch pk.(type) {
			case *packet.AddItemActor, *packet.MoveActorAbsolute:
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
		s.writePacket(&packet.SetActorMotion{EntityRuntimeID: 2})
	}
	waitQueueSignal(t, c.closed, "overloaded connection to close")
	s.writePacket(&packet.Text{Message: "after close"})
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
				s.writePacket(&packet.Text{Message: strconv.Itoa(producer*perProducer + i)})
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
			pk, ok := raw.(*packet.Text)
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

func TestSessionPacketQueueRefillWithFullChannel(t *testing.T) {
	s := &Session{packets: make(chan packet.Packet, 1)}
	s.writePacket(&packet.Text{Message: "first"})
	<-s.packets
	// A producer wins the lock after the consumer freed a slot, filling the
	// channel and then creating an overflow before the consumer can refill it.
	second, third := &packet.Text{Message: "second"}, &packet.Text{Message: "third"}
	s.writePacket(second)
	s.writePacket(third)
	done := make(chan struct{})
	go func() {
		s.refillPackets()
		close(done)
	}()
	waitQueueSignal(t, done, "refill with a full channel to return")
	if got := <-s.packets; got != second {
		t.Fatalf("packet = %v, want %v", got, second)
	}
	s.refillPackets()
	if got := <-s.packets; got != third {
		t.Fatalf("packet = %v, want %v", got, third)
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
	written                  chan packet.Packet
	startOnce, closeOnce     sync.Once
	writeErr                 error
}

func newQueueTestConn() *queueTestConn {
	return &queueTestConn{
		started: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{}),
		written: make(chan packet.Packet, maxQueuedPackets+16),
	}
}

func (c *queueTestConn) WritePacket(pk packet.Packet) error {
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
