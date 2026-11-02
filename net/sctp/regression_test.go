package sctp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"slices"
	"sync/atomic"
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

// inject hands an association a packet of one chunk as its peer sends it.
func inject(a *Association, now time.Time, typ, flags byte, parts ...[]byte) error {
	var p packetBuilder
	p.start(header{srcPort: 5000, dstPort: 5000, tag: a.localTag})
	p.addChunk(typ, flags, parts...)
	return a.HandlePacket(p.finish(), now)
}

// initValue returns the value of an INIT or INIT ACK of a peer with the
// tag and initial TSN given, a window of 1 MiB, a stream each way and the
// parameters given, each made with appendParam.
func initValue(tag, tsn uint32, params ...[]byte) []byte {
	v := binary.BigEndian.AppendUint32(nil, tag)
	v = binary.BigEndian.AppendUint32(v, 1<<20)
	v = binary.BigEndian.AppendUint16(v, 1)
	v = binary.BigEndian.AppendUint16(v, 1)
	v = binary.BigEndian.AppendUint32(v, tsn)
	return slices.Concat(append([][]byte{v}, params...)...)
}

// connectTo opens an association with a peer whose INIT ACK has the
// parameters given besides its cookie. Its packets go nowhere.
func connectTo(t *testing.T, now time.Time, params ...[]byte) *Association {
	t.Helper()
	return connectWith(t, Config{}, now, params...)
}

// connectWith is connectTo for an association configured with cfg.
func connectWith(t *testing.T, cfg Config, now time.Time, params ...[]byte) *Association {
	t.Helper()
	a := New(cfg, func([]byte) {})
	a.Connect(now)
	params = append(params, appendParam(nil, paramStateCookie, []byte("cookie")))
	if err := inject(a, now, chunkInitAck, 0, initValue(0x1234, 100, params...)); err != nil {
		t.Fatal(err)
	}
	if err := inject(a, now, chunkCookieAck, 0); err != nil {
		t.Fatal(err)
	}
	if !a.Established() {
		t.Fatal("not established")
	}
	return a
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
	send(b.assoc.peerCum + tsnWindow + 1)
	if len(b.assoc.received) != 0 {
		t.Fatal("kept a TSN beyond the window")
	}
	send(b.assoc.peerCum + tsnWindow)
	if len(b.assoc.received) != 1 {
		t.Fatal("dropped the last TSN of the window")
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

// TestOversizedHeartbeatHoldsNothingUp covers a HEARTBEAT whose echo
// cannot fit a packet: it goes unanswered, and the chunks queued after it
// still go out, so that a graceful shutdown completes.
func TestOversizedHeartbeatHoldsNothingUp(t *testing.T) {
	n := newNetwork(t, 19, Config{})
	n.open(t, false)
	a, b := n.peers[0], n.peers[1]
	if err := inject(b.assoc, n.now, chunkHeartbeat, 0, make([]byte, 2000)); err != nil {
		t.Fatal(err)
	}
	b.assoc.Shutdown(n.now)
	n.run(time.Minute, func() bool { return a.assoc.Err() != nil && b.assoc.Err() != nil })
	if !errors.Is(a.assoc.Err(), ErrClosed) || !errors.Is(b.assoc.Err(), ErrClosed) {
		t.Fatalf("shutdown ended with %v and %v, want both closed", a.assoc.Err(), b.assoc.Err())
	}
}

// TestHostileShutdownAckReturns covers what a hostile peer may send a
// closing association after an oversized HEARTBEAT: every packet is
// handled in bounded time.
func TestHostileShutdownAckReturns(t *testing.T) {
	n := newNetwork(t, 20, Config{})
	n.open(t, false)
	b := n.peers[1].assoc
	var sent atomic.Int64
	var completed atomic.Bool
	b.send = func(p []byte) {
		sent.Add(1)
		if hasChunk(p, chunkShutdownComplete) {
			completed.Store(true)
		}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = inject(b, n.now, chunkHeartbeat, 0, make([]byte, 2000))
		_ = inject(b, n.now, chunkShutdown, 0, binary.BigEndian.AppendUint32(nil, b.cumAck))
		_ = inject(b, n.now, chunkShutdownAck, 0)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("HandlePacket did not return after %d packets sent", sent.Load())
	}
	if !errors.Is(b.Err(), ErrClosed) || !completed.Load() {
		t.Fatalf("association ended with %v, SHUTDOWN COMPLETE sent: %v; want closed after sending it", b.Err(), completed.Load())
	}
}

// TestPeerPastReceiveWindowFails covers a peer that ignores the window and
// sends a message that never ends: the association fails with an ABORT
// rather than hold its fragments without bound.
func TestPeerPastReceiveWindowFails(t *testing.T) {
	const window = 64000
	n := newNetwork(t, 21, Config{ReceiveWindow: window})
	n.open(t, false)
	b := n.peers[1].assoc
	aborted := false
	b.send = func(p []byte) { aborted = aborted || hasChunk(p, chunkAbort) }
	first := b.peerCum + 1
	var head [12]byte
	taken := uint32(0)
	for ; taken <= 2*window/1000; taken++ {
		binary.BigEndian.PutUint32(head[:], first+taken)
		flags := byte(0)
		if taken == 0 {
			flags = flagBegin
		}
		if err := inject(b, n.now, chunkData, flags, head[:], make([]byte, 1000-dataHeaderSize)); err != nil {
			break
		}
	}
	// Twice the window is held, not a byte more.
	if taken != 2*window/1000 {
		t.Fatalf("took %d chunks of 1000 bytes under a window of %d", taken, window)
	}
	if !aborted {
		t.Fatal("the peer was not sent an ABORT")
	}
}

// TestMessageLargerThanWindowArrives covers a message larger than the
// receive window from a peer that keeps to it: zero window probes carry
// the part past the window.
func TestMessageLargerThanWindowArrives(t *testing.T) {
	const window = 16 << 10
	n := newNetwork(t, 22, Config{ReceiveWindow: window})
	n.open(t, false)
	a, b := n.peers[0], n.peers[1]
	msg := payload(22, window*3/2)
	if err := a.assoc.Send(1, PPIDBinary, msg, SendOptions{}, n.now); err != nil {
		t.Fatal(err)
	}
	n.run(time.Minute, func() bool { return len(b.received) > 0 })
	if len(b.received) != 1 || !bytes.Equal(b.received[0].data, msg) {
		t.Fatalf("received %d messages, errors %v and %v", len(b.received), a.err, b.err)
	}
}

// TestTimeoutRetransmitsWithinWindow covers the expiry of the
// retransmission timer: the window collapses to one packet, and what goes
// again keeps to it (RFC 9260 sections 6.3.3 and 7.2.1) rather than go
// all at once.
func TestTimeoutRetransmitsWithinWindow(t *testing.T) {
	n := newNetwork(t, 23, Config{})
	n.open(t, false)
	a := n.peers[0].assoc
	first := a.nextTSN
	// The first two chunks, of 1116 and 47 bytes, fill the window of one
	// packet left after the timeout; the third would fit the packet of the
	// second.
	sendAll(t, a, n.now, append([]int{1100, 31, 10}, slices.Repeat([]int{1100}, 5)...)...)
	sent := recordData(a)
	for a.Stats().Timeouts == 0 {
		if err := a.HandleTimeout(a.Deadline()); err != nil {
			t.Fatal(err)
		}
	}
	if st := a.Stats(); st.Flight > st.Cwnd+a.cfg.MaxPacketSize-1 || !slices.Equal(*sent, []uint32{first, first + 1}) {
		t.Fatalf("sent %v again, %d bytes in flight after a timeout with a window of %d", *sent, st.Flight, st.Cwnd)
	}
}

// TestFastRetransmitIgnoresFullWindow covers a chunk reported missing
// three times while the window is full: it goes again at once, without
// regard to the window (RFC 9260 section 7.2.4).
func TestFastRetransmitIgnoresFullWindow(t *testing.T) {
	n := newNetwork(t, 24, Config{})
	n.open(t, false)
	a := n.peers[0].assoc
	var sent [][]byte
	a.send = func(p []byte) { sent = append(sent, bytes.Clone(p)) }
	first := a.nextTSN
	for i := range 10 {
		if err := a.Send(1, PPIDBinary, payload(i, 1100), SendOptions{}, n.now); err != nil {
			t.Fatal(err)
		}
	}
	for k := uint16(1); k <= 3; k++ {
		sent = nil
		// The chunks after the first arrived, one more each time.
		sack := appendSack(nil, first-1, 1<<20, []uint16{2, 1 + k}, nil)
		if err := inject(a, n.now, chunkSack, 0, sack); err != nil {
			t.Fatal(err)
		}
	}
	if st := a.Stats(); st.Flight < st.Cwnd {
		t.Fatalf("the window is not full: %d bytes in flight, window %d", st.Flight, st.Cwnd)
	}
	for _, p := range sent {
		if slices.Contains(dataTSNs(p), first) {
			return
		}
	}
	t.Fatal("the chunk missed three times did not go again at once")
}

// TestFragmentFloodTakesLinearTime covers a peer that sends fragments of a
// byte that complete no message: unordered ones that each begin a message,
// and ordered ones of a message whose turn has not come. Each costs the
// same however many are held, rather than a look over all of them.
func TestFragmentFloodTakesLinearTime(t *testing.T) {
	for _, flags := range []byte{flagBegin | flagUnordered, flagBegin} {
		n := newNetwork(t, 39, Config{ReceiveWindow: 4 << 20})
		n.open(t, false)
		b := n.peers[1].assoc
		b.send = func([]byte) {}
		const packets, chunks = 3000, 50
		done := make(chan struct{})
		go func() {
			defer close(done)
			tsn := b.peerCum + 1
			for range packets {
				var p packetBuilder
				p.start(header{srcPort: 5000, dstPort: 5000, tag: b.localTag})
				for range chunks {
					// An ordered fragment has SSN 1, and SSN 0 never comes.
					v := dataValue(tsn, []byte{1})
					binary.BigEndian.PutUint16(v[6:], 1)
					p.addChunk(chunkData, flags, v)
					tsn++
				}
				_ = b.HandlePacket(p.finish(), n.now)
			}
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("flags %#x: %d packets of %d fragments not handled in 5 s", flags, packets, chunks)
		}
		if b.held != packets*chunks*(dataHeaderSize+1) {
			t.Fatalf("flags %#x: holding %d bytes, want %d", flags, b.held, packets*chunks*(dataHeaderSize+1))
		}
	}
}

// TestPartialReliabilityNeedsPeerSupport covers a peer whose INIT ACK does
// not offer FORWARD TSN: it cannot be told to skip a message given up, so a
// partially reliable message goes reliably, again after a loss, until it
// is acknowledged.
func TestPartialReliabilityNeedsPeerSupport(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	a := connectTo(t, now)
	first := a.nextTSN
	sent := recordData(a)
	opts := SendOptions{Unordered: true, Partial: true, Lifetime: time.Millisecond}
	if err := a.Send(1, PPIDBinary, []byte("lost"), opts, now); err != nil {
		t.Fatal(err)
	}
	for a.Stats().Timeouts == 0 {
		if err := a.HandleTimeout(a.Deadline()); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(*sent, []uint32{first, first}) {
		t.Fatalf("sent %v, want %d again after the loss", *sent, first)
	}
	sack(t, a, now, first, 1<<20)
	if a.Buffered() != 0 {
		t.Fatalf("%d bytes buffered after the peer acknowledged the message", a.Buffered())
	}
}

// resetResponses returns the sequence numbers and results of the stream
// reset responses of a packet.
func resetResponses(p []byte) [][2]uint32 {
	var responses [][2]uint32
	for _, c := range packetChunks(p) {
		params, _ := parseParams(c.value)
		for _, param := range params {
			if c.typ == chunkReconfig && param.typ == paramReconfigResponse {
				responses = append(responses, [2]uint32{binary.BigEndian.Uint32(param.value), binary.BigEndian.Uint32(param.value[4:])})
			}
		}
	}
	return responses
}

// outgoingReset returns an Outgoing SSN Reset Request parameter.
func outgoingReset(seq, last uint32, streams ...uint16) []byte {
	v := binary.BigEndian.AppendUint32(nil, seq)
	v = binary.BigEndian.AppendUint32(v, 0)
	v = binary.BigEndian.AppendUint32(v, last)
	for _, s := range streams {
		v = binary.BigEndian.AppendUint16(v, s)
	}
	return appendParam(nil, paramOutgoingReset, v)
}

// TestIncomingResetTakesSequenceNumber covers a peer that asks this side
// to reset the streams it sends on, which this side denies, then resets
// its own: every request takes the next sequence number (RFC 6525 section
// 4.1), and one sent again gets the answer it got (section 5.2.1).
func TestIncomingResetTakesSequenceNumber(t *testing.T) {
	var resets [][]uint16
	n := newNetwork(t, 40, Config{})
	n.peers[1].assoc.cfg.OnStreamReset = func(s []uint16) { resets = append(resets, s) }
	n.open(t, false)
	b := n.peers[1].assoc
	var responses [][2]uint32
	b.send = func(p []byte) { responses = append(responses, resetResponses(p)...) }
	seq := n.peers[0].assoc.initialTSN
	incoming := appendParam(nil, paramIncomingReset, binary.BigEndian.AppendUint16(binary.BigEndian.AppendUint32(nil, seq), 1))
	for range 2 {
		if err := inject(b, n.now, chunkReconfig, 0, incoming); err != nil {
			t.Fatal(err)
		}
	}
	if err := inject(b, n.now, chunkReconfig, 0, outgoingReset(seq+1, b.peerCum, 1)); err != nil {
		t.Fatal(err)
	}
	want := [][2]uint32{{seq, resultDenied}, {seq, resultDenied}, {seq + 1, resultPerformed}}
	if !slices.Equal(responses, want) || len(resets) != 1 {
		t.Fatalf("responses %v, %d resets; want %v and one", responses, len(resets), want)
	}
}

// TestForwardTSNDeliversStrandedMessage covers an ordered message that
// arrived whole behind one lost, both given up by the sender, which saw no
// SACK: the FORWARD TSN that skips them lets the one that arrived take its
// turn (RFC 3758 section 3.6) rather than drop it.
func TestForwardTSNDeliversStrandedMessage(t *testing.T) {
	n := newNetwork(t, 41, Config{})
	n.open(t, false)
	a, b := n.peers[0], n.peers[1]
	first := a.assoc.nextTSN
	forwarded := false
	n.drop = func(to int, p []byte) bool {
		forwarded = forwarded || to == 1 && hasChunk(p, chunkForwardTSN)
		return to == 1 && slices.Contains(dataTSNs(p), first) || to == 0 && !forwarded
	}
	for i := range 2 {
		if err := a.assoc.Send(1, PPIDBinary, []byte{byte(i)}, SendOptions{Partial: true}, n.now); err != nil {
			t.Fatal(err)
		}
	}
	n.run(time.Minute, func() bool { return a.assoc.Buffered() == 0 })
	if !forwarded || len(b.received) != 1 || b.received[0].data[0] != 1 {
		t.Fatalf("FORWARD TSN sent: %v; delivered %v, want the second message", forwarded, b.received)
	}
}

// TestMessageGivenUpPartlySentIsSkipped covers an ordered message of
// limited lifetime that expires while waiting for the window with its
// first chunks sent and acknowledged: the peer, which holds them, must be
// told to skip the message, or the stream stays blocked behind it.
func TestMessageGivenUpPartlySentIsSkipped(t *testing.T) {
	n := newNetwork(t, 43, Config{})
	n.open(t, false)
	a, b := n.peers[0], n.peers[1]
	// The SACKs of the peer are held back until the lifetime passed, and
	// the window they open goes to the rest of the message.
	var held [][]byte
	hold := true
	n.drop = func(to int, p []byte) bool {
		if to == 0 && hold && hasChunk(p, chunkSack) {
			held = append(held, p)
			return true
		}
		return false
	}
	opts := SendOptions{Partial: true, MaxRetransmits: 1 << 30, Lifetime: 100 * time.Millisecond}
	sendAll(t, a.assoc, n.now, slices.Repeat([]int{1100}, 10)...)
	if err := a.assoc.Send(2, PPIDBinary, payload(1, 3000), opts, n.now); err != nil {
		t.Fatal(err)
	}
	n.run(50*time.Millisecond, func() bool { return false })
	hold = false
	n.now = n.now.Add(time.Second)
	for _, p := range held {
		if err := a.assoc.HandlePacket(p, n.now); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.assoc.Send(2, PPIDBinary, []byte("next"), SendOptions{}, n.now); err != nil {
		t.Fatal(err)
	}
	n.run(time.Minute, func() bool { return len(b.received) == 11 && a.assoc.Buffered() == 0 })
	if len(b.received) != 11 || string(b.received[10].data) != "next" {
		t.Fatalf("delivered %d messages, the last of %d bytes; want 11, the last the one after the message given up", len(b.received), len(b.received[len(b.received)-1].data))
	}
	if a.assoc.Buffered() != 0 {
		t.Fatalf("%d bytes buffered once everything was acknowledged or given up", a.assoc.Buffered())
	}
}

// TestRTTMeasuredAfterTimedChunkGivenUp covers a timed chunk given up
// with the message it is part of, missed with it: the next chunk sent is
// timed, and the round trip measured again (RFC 9260 section 6.3.1, rule
// C4).
func TestRTTMeasuredAfterTimedChunkGivenUp(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	a := connectTo(t, now, appendParam(nil, paramForwardTSN, nil))
	first := a.nextTSN
	// A byte, timed, and ten chunks fill the window but for the first chunk
	// of a message of two.
	sendAll(t, a, now, append([]int{1}, slices.Repeat([]int{1131}, 10)...)...)
	if err := a.Send(2, PPIDBinary, payload(1, 2000), SendOptions{Unordered: true, Partial: true}, now); err != nil {
		t.Fatal(err)
	}
	// The byte gives a sample of 1 s; the window lets the second chunk of
	// the message go, timed.
	now = now.Add(time.Second)
	sack(t, a, now, first+10, 1<<20)
	sendAll(t, a, now, 1, 1, 1)
	// Both chunks of the message are missed three times: it is given up.
	for k := uint16(3); k <= 5; k++ {
		sack(t, a, now, first+10, 1<<20, 3, k)
	}
	now = now.Add(time.Second)
	sack(t, a, now, first+15, 1<<20)
	sendAll(t, a, now, 1)
	sack(t, a, now.Add(2*time.Second), first+16, 1<<20)
	if want := time.Second + time.Second/8; a.RTT() != want {
		t.Fatalf("RTT %v, want %v from a second sample of 2 s", a.RTT(), want)
	}
}

// TestHeldFragmentsCountTheirHeaders covers a peer that sends fragments of
// a byte that complete nothing: each takes room in the window as the chunk
// it came in, header and all, so that what the association holds before it
// fails stays in proportion to the window.
func TestHeldFragmentsCountTheirHeaders(t *testing.T) {
	const window, chunk = 1700, dataHeaderSize + 1
	n := newNetwork(t, 42, Config{ReceiveWindow: window})
	n.open(t, false)
	b := n.peers[1].assoc
	var windows []uint32
	aborted := false
	b.send = func(p []byte) {
		if s, ok := sackIn(p); ok {
			windows = append(windows, s.window)
		}
		aborted = aborted || hasChunk(p, chunkAbort)
	}
	taken := 0
	for inject(b, n.now, chunkData, flagBegin|flagImmediate, dataValue(b.peerCum+1, []byte{1})) == nil {
		taken++
	}
	if windows[0] != window-chunk || taken != 2*window/chunk || !aborted {
		t.Fatalf("window %d after a fragment, %d taken before the abort (sent: %v); want %d and %d", windows[0], taken, aborted, window-chunk, 2*window/chunk)
	}
}

// TestErrorCountResetByChunkGivenUpAcked covers a SACK that acknowledges
// a chunk given up: it acknowledges a DATA chunk sent, and the count of
// timeouts in a row that fails the association starts over (RFC 9260
// section 8.1).
func TestErrorCountResetByChunkGivenUpAcked(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	a := connectWith(t, Config{MaxRetransmissions: 1}, now, appendParam(nil, paramForwardTSN, nil))
	first := a.nextTSN
	sendAll(t, a, now, 1)
	if err := a.Send(2, PPIDBinary, []byte{2}, SendOptions{Partial: true}, now); err != nil {
		t.Fatal(err)
	}
	// The timeout gives the second chunk up; the peer had it.
	if err := a.HandleTimeout(a.Deadline()); err != nil {
		t.Fatal(err)
	}
	sack(t, a, a.Deadline(), first-1, 1<<20, 2, 2)
	if err := a.HandleTimeout(a.Deadline()); err != nil {
		t.Fatalf("the timeout after the SACK failed the association: %v", err)
	}
}
