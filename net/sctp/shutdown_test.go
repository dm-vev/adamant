package sctp

import (
	"errors"
	"testing"
	"time"
)

// countChunks returns how many chunks of a type a packet carries.
func countChunks(p []byte, typ byte) int {
	n := 0
	for _, c := range packetChunks(p) {
		if c.typ == typ {
			n++
		}
	}
	return n
}

// TestShutdownRetransmissionsBounded covers a peer that never answers the
// SHUTDOWN: it goes MaxRetransmissions times again, then the association
// fails.
func TestShutdownRetransmissionsBounded(t *testing.T) {
	n := newNetwork(t, 60, Config{MaxRetransmissions: 2})
	n.open(t, false)
	a := n.peers[0]
	shutdowns := 0
	n.drop = func(to int, p []byte) bool {
		shutdowns += countChunks(p, chunkShutdown)
		return to == 1
	}
	a.assoc.Shutdown(n.now)
	n.run(time.Hour, func() bool { return a.err != nil })
	if shutdowns != 3 || !errors.Is(a.err, ErrTimeout) {
		t.Fatalf("%d SHUTDOWNs sent, then %v; want 3 and a timeout", shutdowns, a.err)
	}
}

// TestShutdownAckSentAgain covers a SHUTDOWN ACK lost twice: it goes again
// on its timer and to the SHUTDOWN sent again, and the shutdown completes.
func TestShutdownAckSentAgain(t *testing.T) {
	n := newNetwork(t, 61, Config{})
	n.open(t, false)
	a, b := n.peers[0], n.peers[1]
	acks := 0
	n.drop = func(to int, p []byte) bool {
		if to == 0 && hasChunk(p, chunkShutdownAck) {
			acks++
			return acks <= 2
		}
		return false
	}
	a.assoc.Shutdown(n.now)
	n.run(time.Minute, func() bool { return a.assoc.Err() != nil && b.assoc.Err() != nil })
	if !errors.Is(a.assoc.Err(), ErrClosed) || !errors.Is(b.assoc.Err(), ErrClosed) || acks != 3 {
		t.Fatalf("closed with %v and %v after %d SHUTDOWN ACKs, want 3", a.assoc.Err(), b.assoc.Err(), acks)
	}
}

// TestShutdownCollision covers both sides shutting down at once: each
// answers the SHUTDOWN of the other with a SHUTDOWN ACK (RFC 9260 section
// 9.2), sent again MaxRetransmissions times while it is lost.
func TestShutdownCollision(t *testing.T) {
	n := newNetwork(t, 62, Config{MaxRetransmissions: 2})
	n.open(t, false)
	a, b := n.peers[0], n.peers[1]
	acks := [2]int{}
	n.drop = func(to int, p []byte) bool {
		acks[1-to] += countChunks(p, chunkShutdownAck)
		return hasChunk(p, chunkShutdownAck)
	}
	a.assoc.Shutdown(n.now)
	b.assoc.Shutdown(n.now)
	n.run(time.Hour, func() bool { return a.err != nil && b.err != nil })
	if acks != [2]int{3, 3} || !errors.Is(a.err, ErrTimeout) || !errors.Is(b.err, ErrTimeout) {
		t.Fatalf("SHUTDOWN ACKs sent %v, then %v and %v; want 3 each and timeouts", acks, a.err, b.err)
	}
	n = newNetwork(t, 63, Config{})
	n.open(t, false)
	n.peers[0].assoc.Shutdown(n.now)
	n.peers[1].assoc.Shutdown(n.now)
	n.run(time.Minute, func() bool { return n.peers[0].assoc.Err() != nil && n.peers[1].assoc.Err() != nil })
	if !errors.Is(n.peers[0].assoc.Err(), ErrClosed) || !errors.Is(n.peers[1].assoc.Err(), ErrClosed) {
		t.Fatalf("closed with %v and %v without loss", n.peers[0].assoc.Err(), n.peers[1].assoc.Err())
	}
}

// TestShutdownWaitsForDataInFlight covers a SHUTDOWN that arrives while
// this side has data in flight: the SHUTDOWN ACK waits until the data is
// acknowledged, and the side shutting down answers that data with a SACK
// and a SHUTDOWN at once (RFC 9260 section 9.2).
func TestShutdownWaitsForDataInFlight(t *testing.T) {
	n := newNetwork(t, 64, Config{})
	n.open(t, false)
	a, b := n.peers[0], n.peers[1]
	lost, early, answered := false, false, false
	n.drop = func(to int, p []byte) bool {
		if to == 0 && len(dataTSNs(p)) > 0 && !lost {
			lost = true
			return true
		}
		early = early || to == 0 && hasChunk(p, chunkShutdownAck) && len(a.received) == 0
		if to == 1 && len(a.received) > 0 && !answered {
			answered = true
			if !hasChunk(p, chunkSack) || !hasChunk(p, chunkShutdown) {
				t.Errorf("answered the DATA with %v, want a SACK and a SHUTDOWN", chunksSent([][]byte{p}))
			}
		}
		return false
	}
	if err := b.assoc.Send(1, PPIDBinary, []byte("late"), SendOptions{}, n.now); err != nil {
		t.Fatal(err)
	}
	a.assoc.Shutdown(n.now)
	n.run(time.Minute, func() bool { return a.assoc.Err() != nil && b.assoc.Err() != nil })
	if early || len(a.received) != 1 || !errors.Is(a.assoc.Err(), ErrClosed) || !errors.Is(b.assoc.Err(), ErrClosed) {
		t.Fatalf("SHUTDOWN ACK before the data: %v; delivered %d; closed with %v and %v", early, len(a.received), a.assoc.Err(), b.assoc.Err())
	}
}

// TestShutdownAcknowledgesData covers the cumulative TSN of a SHUTDOWN: it
// acknowledges data in flight like a SACK and restarts the retransmission
// timer, unless it is older than the last or acknowledges data never sent.
func TestShutdownAcknowledgesData(t *testing.T) {
	n := newNetwork(t, 65, Config{})
	n.open(t, false)
	b := n.peers[1].assoc
	b.send = func([]byte) {}
	first := b.nextTSN
	sendAll(t, b, n.now, 100, 200)
	shutdown := func(cum uint32, now time.Time) {
		t.Helper()
		if err := inject(b, now, chunkShutdown, 0, appendSack(nil, cum, 0, nil, nil)[:4]); err != nil {
			t.Fatal(err)
		}
	}
	shutdown(first+2, n.now)
	shutdown(first-2, n.now)
	if b.Buffered() != 300 {
		t.Fatalf("%d bytes buffered after SHUTDOWNs acknowledging data never sent or older, want 300", b.Buffered())
	}
	later := n.now.Add(200 * time.Millisecond)
	shutdown(first, later)
	if b.Buffered() != 200 || !b.Deadline().Equal(later.Add(b.Stats().RTO)) {
		t.Fatalf("%d bytes buffered, timer at %v; want 200 and restarted at %v", b.Buffered(), b.Deadline().Sub(n.now), later.Add(b.Stats().RTO).Sub(n.now))
	}
}
