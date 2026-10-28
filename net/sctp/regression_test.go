package sctp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
	"time"
)

func packetChunks(b []byte) []chunk {
	_, chunks, err := parsePacket(b, nil)
	if err != nil {
		return nil
	}
	return chunks
}

func hasChunk(b []byte, typ byte) bool {
	for _, c := range packetChunks(b) {
		if c.typ == typ {
			return true
		}
	}
	return false
}

// dataTSNs returns the TSNs of the DATA chunks of a packet.
func dataTSNs(b []byte) []uint32 {
	var tsns []uint32
	for _, c := range packetChunks(b) {
		if c.typ == chunkData && len(c.value) >= 4 {
			tsns = append(tsns, binary.BigEndian.Uint32(c.value))
		}
	}
	return tsns
}

// TestForwardTSNSentAgainWhenLost covers a partially reliable message
// abandoned by fast retransmit accounting: once nothing is left
// unacknowledged, the FORWARD TSN must still be repeated if lost, or the
// peer's cumulative TSN stays stuck.
func TestForwardTSNSentAgainWhenLost(t *testing.T) {
	n := newNetwork(t, 11, Config{})
	n.open(t, false)
	a, b := n.peers[0], n.peers[1]
	first := a.assoc.nextTSN
	droppedForward := false
	n.drop = func(to int, p []byte) bool {
		if to == 1 && len(dataTSNs(p)) > 0 && dataTSNs(p)[0] == first {
			return true
		}
		if to == 1 && hasChunk(p, chunkForwardTSN) && !droppedForward {
			droppedForward = true
			return true
		}
		return false
	}
	for i := range 6 {
		if err := a.assoc.Send(3, PPIDBinary, []byte{byte(i)}, SendOptions{Unordered: true, Partial: true}, n.now); err != nil {
			t.Fatal(err)
		}
		n.run(30*time.Millisecond, func() bool { return false })
	}
	n.run(time.Minute, func() bool { return a.assoc.Buffered() == 0 && b.assoc.peerCum == first+5 })
	if !droppedForward {
		t.Fatal("no FORWARD TSN was sent")
	}
	if b.assoc.peerCum != first+5 || a.assoc.Buffered() != 0 {
		t.Fatalf("peer cumulative TSN %d, want %d; %d bytes buffered", b.assoc.peerCum-first, 5, a.assoc.Buffered())
	}
	if len(b.received) != 5 {
		t.Fatalf("received %d of the 5 messages that were not lost", len(b.received))
	}
}

// TestRepeatedSACKAddsNoMisses checks the HTNA rule: SACKs that repeat the
// same gap report do not count toward fast retransmit.
func TestRepeatedSACKAddsNoMisses(t *testing.T) {
	n := newNetwork(t, 12, Config{})
	n.open(t, false)
	a := n.peers[0]
	first := a.assoc.nextTSN
	n.drop = func(to int, p []byte) bool {
		return to == 1 && len(dataTSNs(p)) > 0 && dataTSNs(p)[0] == first
	}
	n.dup = 1
	for i := range 3 {
		if err := a.assoc.Send(1, PPIDBinary, []byte{byte(i)}, SendOptions{}, n.now); err != nil {
			t.Fatal(err)
		}
		n.run(15*time.Millisecond, func() bool { return false })
	}
	// Two chunks after the lost one were newly acknowledged, each SACK
	// arriving twice: two misses, not four.
	ch := a.assoc.outstanding[0]
	if ch.tsn != first || ch.misses != 2 || ch.fastDone {
		t.Fatalf("lost chunk: misses %d, fast retransmitted %v", ch.misses, ch.fastDone)
	}
}

// TestRenegedChunkIsUnacknowledged checks that a chunk a SACK no longer
// reports as received is treated as unacknowledged again.
func TestRenegedChunkIsUnacknowledged(t *testing.T) {
	n := newNetwork(t, 13, Config{})
	n.open(t, false)
	a := n.peers[0]
	first := a.assoc.nextTSN
	n.drop = func(to int, p []byte) bool { return to == 1 }
	for i := range 3 {
		if err := a.assoc.Send(1, PPIDBinary, []byte{byte(i)}, SendOptions{}, n.now); err != nil {
			t.Fatal(err)
		}
	}
	sack := func(gaps ...uint16) {
		var p packetBuilder
		p.start(header{srcPort: 5000, dstPort: 5000, tag: a.assoc.localTag})
		p.addChunk(chunkSack, 0, appendSack(nil, first-1, 1<<20, gaps, nil))
		if err := a.assoc.HandlePacket(p.finish(), n.now); err != nil {
			t.Fatal(err)
		}
	}
	sack(2, 2)
	if !a.assoc.outstanding[1].acked {
		t.Fatal("the gap ack did not mark the chunk")
	}
	sack()
	if a.assoc.outstanding[1].acked {
		t.Fatal("a chunk the peer reneged on stayed acknowledged")
	}
	if a.assoc.t3.IsZero() {
		t.Fatal("no retransmission timer for the reneged chunk")
	}
}

func TestEmptyMessageRejected(t *testing.T) {
	n := newNetwork(t, 14, Config{})
	n.open(t, false)
	if err := n.peers[0].assoc.Send(1, PPIDBinary, nil, SendOptions{}, n.now); !errors.Is(err, errEmpty) {
		t.Fatalf("empty message: %v", err)
	}
}

// TestTimedPartialReliability checks that messages with a lifetime are
// abandoned once it passes and the association moves on.
func TestTimedPartialReliability(t *testing.T) {
	n := newNetwork(t, 15, Config{})
	n.open(t, false)
	a, b := n.peers[0], n.peers[1]
	n.loss = 1
	opts := SendOptions{Unordered: true, Partial: true, MaxRetransmits: 1 << 30, Lifetime: 300 * time.Millisecond}
	for i := range 20 {
		if err := a.assoc.Send(3, PPIDBinary, []byte{byte(i)}, opts, n.now); err != nil {
			t.Fatal(err)
		}
	}
	n.run(2*time.Second, func() bool { return false })
	n.loss = 0
	if err := a.assoc.Send(1, PPIDBinary, []byte("after"), SendOptions{}, n.now); err != nil {
		t.Fatal(err)
	}
	n.run(time.Minute, func() bool { return a.assoc.Buffered() == 0 && len(b.received) > 0 })
	if a.assoc.Buffered() != 0 {
		t.Fatalf("%d bytes buffered after the lifetime passed", a.assoc.Buffered())
	}
	if len(b.received) != 1 || string(b.received[0].data) != "after" {
		t.Fatalf("received %d messages, want only the reliable one", len(b.received))
	}
}

// TestOutOfOrderStateBounded checks that a peer cannot grow the receive
// state with TSNs far ahead of the cumulative TSN.
func TestOutOfOrderStateBounded(t *testing.T) {
	n := newNetwork(t, 16, Config{})
	n.open(t, false)
	b := n.peers[1]
	send := func(tsn uint32) {
		var p packetBuilder
		p.start(header{srcPort: 5000, dstPort: 5000, tag: b.assoc.localTag})
		var head [12]byte
		binary.BigEndian.PutUint32(head[:], tsn)
		p.addChunk(chunkData, flagBegin|flagEnd|flagUnordered, head[:], []byte{1})
		_ = b.assoc.HandlePacket(p.finish(), n.now)
	}
	send(b.assoc.peerCum + tsnWindow + 10)
	if len(b.assoc.received) != 0 {
		t.Fatal("kept a TSN beyond the window")
	}
	for i := range uint32(maxOutOfOrder + 100) {
		send(b.assoc.peerCum + 2 + 2*i)
	}
	if len(b.assoc.received) != maxOutOfOrder {
		t.Fatalf("holding %d out of order TSNs, want at most %d", len(b.assoc.received), maxOutOfOrder)
	}
}

// TestShutdownDeliversDataInFlight checks that a graceful shutdown over a
// lossy path delivers everything queued on both sides first.
func TestShutdownDeliversDataInFlight(t *testing.T) {
	for seed := range uint64(6) {
		n := newNetwork(t, 20+seed, Config{})
		n.open(t, false)
		a, b := n.peers[0], n.peers[1]
		n.loss, n.shuffle = 0.2, true
		for i := range 40 {
			if err := a.assoc.Send(1, PPIDBinary, payload(i, 3000), SendOptions{}, n.now); err != nil {
				t.Fatal(err)
			}
			if err := b.assoc.Send(1, PPIDBinary, payload(1000+i, 500), SendOptions{}, n.now); err != nil {
				t.Fatal(err)
			}
		}
		a.assoc.Shutdown(n.now)
		if err := a.assoc.Send(1, PPIDBinary, []byte{1}, SendOptions{}, n.now); err == nil {
			t.Fatal("accepted a message after Shutdown")
		}
		n.run(10*time.Minute, func() bool { return a.assoc.Err() != nil && b.assoc.Err() != nil })
		if !errors.Is(a.assoc.Err(), ErrClosed) || !errors.Is(b.assoc.Err(), ErrClosed) {
			t.Fatalf("seed %d: errors %v %v", seed, a.assoc.Err(), b.assoc.Err())
		}
		if len(b.received) != 40 || len(a.received) != 40 {
			t.Fatalf("seed %d: delivered %d and %d of 40 before closing", seed, len(b.received), len(a.received))
		}
		for i := range 40 {
			if !bytes.Equal(b.received[i].data, payload(i, 3000)) || !bytes.Equal(a.received[i].data, payload(1000+i, 500)) {
				t.Fatalf("seed %d: message %d corrupt or out of order", seed, i)
			}
		}
	}
}

// TestStreamReset covers both directions of a stream reset: the request
// waits for the data sent before it, a repeated request is not applied
// twice, the held bytes are released, and the stream starts again at SSN 0.
func TestStreamReset(t *testing.T) {
	var resets, done [][]uint16
	n := newNetwork(t, 17, Config{})
	n.peers[1].assoc.cfg.OnStreamReset = func(s []uint16) { resets = append(resets, s) }
	n.peers[0].assoc.cfg.OnResetDone = func(s []uint16) { done = append(done, s) }
	n.open(t, false)
	a, b := n.peers[0], n.peers[1]

	// The message before the reset is lost once, and so is the first
	// response: the request arrives ahead of the data, and again later.
	first := a.assoc.nextTSN
	lostData, lostResponse := false, false
	n.drop = func(to int, p []byte) bool {
		if to == 1 && !lostData && len(dataTSNs(p)) > 0 && dataTSNs(p)[0] == first {
			lostData = true
			return true
		}
		if to == 0 && !lostResponse && hasChunk(p, chunkReconfig) {
			lostResponse = true
			return true
		}
		return false
	}
	if err := a.assoc.Send(1, PPIDBinary, payload(1, 5000), SendOptions{}, n.now); err != nil {
		t.Fatal(err)
	}
	if err := a.assoc.ResetStreams([]uint16{1}, n.now); err != nil {
		t.Fatal(err)
	}
	if err := a.assoc.Send(1, PPIDBinary, []byte{1}, SendOptions{}, n.now); !errors.Is(err, ErrStreamResetting) {
		t.Fatalf("send during the reset: %v", err)
	}
	n.run(time.Minute, func() bool { return len(done) > 0 })
	if len(done) != 1 || len(resets) != 1 {
		t.Fatalf("resets done %d, applied %d, want 1 and 1", len(done), len(resets))
	}
	if len(b.received) != 1 || !bytes.Equal(b.received[0].data, payload(1, 5000)) {
		t.Fatal("the message sent before the reset was not delivered first")
	}
	if b.assoc.held != 0 {
		t.Fatalf("%d bytes still held after the reset", b.assoc.held)
	}
	// The stream starts over at SSN 0 on both sides.
	if err := a.assoc.Send(1, PPIDBinary, []byte("again"), SendOptions{}, n.now); err != nil {
		t.Fatal(err)
	}
	n.run(time.Minute, func() bool { return len(b.received) == 2 })
	if len(b.received) != 2 || string(b.received[1].data) != "again" {
		t.Fatal("the stream did not start over after the reset")
	}
}

// TestGapAcknowledgedAtOnce checks that while gaps remain each packet is
// acknowledged without the SACK delay, so that a sender recovering several
// losses gets an answer per retransmission.
func TestGapAcknowledgedAtOnce(t *testing.T) {
	n := newNetwork(t, 18, Config{})
	n.open(t, false)
	a, b := n.peers[0], n.peers[1]
	first := a.assoc.nextTSN
	// Every copy of the two chunks is lost, retransmissions too.
	n.drop = func(to int, p []byte) bool {
		if to != 1 {
			return false
		}
		for _, tsn := range dataTSNs(p) {
			if tsn == first || tsn == first+2 {
				return true
			}
		}
		return false
	}
	for i := range 6 {
		if err := a.assoc.Send(1, PPIDBinary, []byte{byte(i)}, SendOptions{}, n.now); err != nil {
			t.Fatal(err)
		}
		n.run(time.Millisecond, func() bool { return false })
	}
	// Fill the first gap by hand: the receiver must SACK at once since the
	// second gap remains.
	var p packetBuilder
	p.start(header{srcPort: 5000, dstPort: 5000, tag: b.assoc.localTag})
	var head [12]byte
	binary.BigEndian.PutUint32(head[:], first)
	binary.BigEndian.PutUint16(head[4:], 1)
	p.addChunk(chunkData, flagBegin|flagEnd, head[:], []byte{0})
	sent := n.sent
	if err := b.assoc.HandlePacket(p.finish(), n.now); err != nil {
		t.Fatal(err)
	}
	if n.sent == sent || !b.assoc.sackAt.IsZero() {
		t.Fatal("the packet that closed one gap of two was not acknowledged at once")
	}
}
