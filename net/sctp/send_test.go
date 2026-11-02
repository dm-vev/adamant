package sctp

import (
	"bytes"
	"errors"
	"slices"
	"testing"
	"time"
)

// recordData has an association record the TSNs of the DATA chunks it
// sends from now on, its packets going nowhere.
func recordData(a *Association) *[]uint32 {
	var tsns []uint32
	a.send = func(p []byte) { tsns = append(tsns, dataTSNs(p)...) }
	return &tsns
}

// sendAll queues messages of the sizes given on stream 1.
func sendAll(t *testing.T, a *Association, now time.Time, sizes ...int) {
	t.Helper()
	for i, size := range sizes {
		if err := a.Send(1, PPIDBinary, payload(i, size), SendOptions{}, now); err != nil {
			t.Fatal(err)
		}
	}
}

// sack hands an association a SACK with the cumulative TSN, window and
// gap ack blocks given.
func sack(t *testing.T, a *Association, now time.Time, cum, window uint32, gaps ...uint16) {
	t.Helper()
	if err := inject(a, now, chunkSack, 0, appendSack(nil, cum, window, gaps, nil)); err != nil {
		t.Fatal(err)
	}
}

// TestNewDataStopsAtFullWindow covers new data against the congestion
// window: none goes once cwnd bytes are in flight (RFC 9260 section
// 7.2.1).
func TestNewDataStopsAtFullWindow(t *testing.T) {
	n := newNetwork(t, 27, Config{})
	n.open(t, false)
	a := n.peers[0].assoc
	sent := recordData(a)
	// Ten chunks of 1147 bytes and one of 160 fill the initial window of
	// ten packets of 1163 bytes exactly.
	sizes := append(slices.Repeat([]int{1131}, 10), 144, 10)
	sendAll(t, a, n.now, sizes...)
	if len(*sent) != 11 || a.Stats().Flight != a.Stats().Cwnd {
		t.Fatalf("sent %d chunks, %d bytes in flight, window %d; want 11 filling it", len(*sent), a.Stats().Flight, a.Stats().Cwnd)
	}
}

// TestZeroPeerWindowHoldsNewData covers the window of the peer: new data
// waits while it is zero with data in flight, and goes once it opens.
func TestZeroPeerWindowHoldsNewData(t *testing.T) {
	n := newNetwork(t, 28, Config{})
	n.open(t, false)
	a := n.peers[0].assoc
	first := a.nextTSN
	sent := recordData(a)
	sendAll(t, a, n.now, 100)
	flight := uint32(a.Stats().Flight)
	sack(t, a, n.now, first-1, flight)
	sendAll(t, a, n.now, 100)
	if len(*sent) != 1 {
		t.Fatalf("sent %d chunks into a zero window", len(*sent))
	}
	sack(t, a, n.now, first-1, flight+1)
	if !slices.Equal(*sent, []uint32{first, first + 1}) {
		t.Fatalf("sent %v once the window opened, want %v", *sent, []uint32{first, first + 1})
	}
}

// TestFullSizeChunksFitExactly covers a packet size that DATA and control
// chunks fill to the byte: a chunk that fits exactly goes, again too, and
// a HEARTBEAT whose echo fits exactly is answered.
func TestFullSizeChunksFitExactly(t *testing.T) {
	// A packet of 1164 bytes takes a common header and a chunk of 1152.
	n := newNetwork(t, 29, Config{MaxPacketSize: 1164})
	n.open(t, false)
	a := n.peers[0].assoc
	first := a.nextTSN
	sent := recordData(a)
	sendAll(t, a, n.now, a.maxPayload)
	for a.Stats().Timeouts == 0 {
		if err := a.HandleTimeout(a.Deadline()); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(*sent, []uint32{first, first}) {
		t.Fatalf("sent %v, want the full chunk %d twice", *sent, first)
	}
	var echo []byte
	a.send = func(p []byte) {
		for _, c := range packetChunks(p) {
			if c.typ == chunkHeartbeatAck {
				echo = c.value
			}
		}
	}
	info := payload(29, 1148)
	if err := inject(a, n.now, chunkHeartbeat, 0, info); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(echo, info) {
		t.Fatalf("echoed %d bytes of a HEARTBEAT of %d that fits exactly", len(echo), len(info))
	}
}

// TestHeartbeatEchoPadded covers the echo of a HEARTBEAT of a length not
// a multiple of four: its value comes back with zero padding (RFC 9260
// section 3.2).
func TestHeartbeatEchoPadded(t *testing.T) {
	n := newNetwork(t, 30, Config{})
	n.open(t, false)
	b := n.peers[1].assoc
	var sent []byte
	b.send = func(p []byte) { sent = bytes.Clone(p) }
	if err := inject(b, n.now, chunkHeartbeat, 0, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	// The common header, then the chunk header and five bytes padded to
	// eight.
	want := append([]byte{chunkHeartbeatAck, 0, 0, 9}, "hello\x00\x00\x00"...)
	if len(sent) != commonHeaderSize+len(want) || !bytes.Equal(sent[commonHeaderSize:], want) {
		t.Fatalf("sent % x, want the echo % x", sent[min(len(sent), commonHeaderSize):], want)
	}
}

// TestFastRetransmitOnce covers loss recovery by miss indications (RFC
// 9260 sections 7.2.3 and 7.2.4): the chunk missed three times goes again
// once, however many more SACKs miss it, no chunk acknowledged goes again,
// and the window grows again once everything outstanding at the loss is
// acknowledged.
func TestFastRetransmitOnce(t *testing.T) {
	n := newNetwork(t, 31, Config{})
	n.open(t, false)
	a := n.peers[0].assoc
	first := a.nextTSN
	sendAll(t, a, n.now, slices.Repeat([]int{1100}, 10)...)
	sent := recordData(a)
	for k := uint16(1); k <= 5; k++ {
		sack(t, a, n.now, first-1, 1<<20, 2, 1+k)
	}
	if !slices.Equal(*sent, []uint32{first}) {
		t.Fatalf("sent %v again, want only %d once", *sent, first)
	}
	recovery := a.Stats().Cwnd
	sack(t, a, n.now, first+9, 1<<20)
	if a.Stats().Cwnd <= recovery {
		t.Fatalf("window %d after the recovery ended, %d during it", a.Stats().Cwnd, recovery)
	}
}

// TestFastRetransmitAfterTimeoutWindowFloor covers a loss found by miss
// indications while the window is small after a timeout: the slow start
// threshold, and the window with it, does not go under four packets (RFC
// 9260 section 7.2.3).
func TestFastRetransmitAfterTimeoutWindowFloor(t *testing.T) {
	n := newNetwork(t, 32, Config{})
	n.open(t, false)
	a := n.peers[0].assoc
	first := a.nextTSN
	sendAll(t, a, n.now, slices.Repeat([]int{1100}, 10)...)
	a.send = func([]byte) {}
	for a.Stats().Timeouts == 0 {
		if err := a.HandleTimeout(a.Deadline()); err != nil {
			t.Fatal(err)
		}
	}
	for k := uint16(1); k <= 3; k++ {
		sack(t, a, n.now, first-1, 1<<20, 2, 1+k)
	}
	if st := a.Stats(); st.Cwnd != 4*a.cfg.MaxPacketSize {
		t.Fatalf("window %d after a fast retransmit following a timeout, want %d", st.Cwnd, 4*a.cfg.MaxPacketSize)
	}
}

// TestTimedMessageExpiresUnsent covers a message of limited lifetime that
// waits for the window past its lifetime, of a nanosecond too: it is
// dropped without a byte of it sent or a FORWARD TSN, its room in the send
// buffer comes back, and the message after it goes.
func TestTimedMessageExpiresUnsent(t *testing.T) {
	for _, lifetime := range []time.Duration{100 * time.Millisecond, time.Nanosecond} {
		n := newNetwork(t, 33, Config{})
		n.open(t, false)
		a := n.peers[0].assoc
		first := a.nextTSN
		// Eleven messages fill the window, the twelfth waits.
		sendAll(t, a, n.now, slices.Repeat([]int{1100}, 12)...)
		opts := SendOptions{Partial: true, MaxRetransmits: 5, Lifetime: lifetime}
		if err := a.Send(2, PPIDBinary, []byte("late"), opts, n.now); err != nil {
			t.Fatal(err)
		}
		if err := a.Send(3, PPIDBinary, []byte("after"), SendOptions{}, n.now); err != nil {
			t.Fatal(err)
		}
		sent := recordPackets(a)
		// Everything sent is acknowledged once the lifetime passed.
		last := first + uint32(len(a.outstanding)) - 1
		sack(t, a, n.now.Add(time.Second), last, 1<<20)
		var tsns []uint32
		for _, p := range *sent {
			tsns = append(tsns, dataTSNs(p)...)
		}
		if !slices.Equal(tsns, []uint32{last + 1, last + 2}) || slices.Contains(chunksSent(*sent), chunkForwardTSN) || a.Buffered() != 1105 {
			t.Fatalf("lifetime %v: sent %v and %v, %d bytes buffered; want the twelfth and the next message alone", lifetime, tsns, chunksSent(*sent), a.Buffered())
		}
	}
}

// TestSmallRoomInPacket covers the room a chunk leaves at the end of a
// packet: a message that fits it whole goes in it, and a longer one starts
// a fragment in it only if the room takes 64 bytes at least.
func TestSmallRoomInPacket(t *testing.T) {
	// A first chunk of 1080 bytes leaves 52 of room in a packet of 1163,
	// one of 1068 leaves 64.
	cases := []struct {
		name     string
		sizes    []int
		together bool
	}{
		{"whole in a small room", []int{1064, 52}, true},
		{"no fragment in a small room", []int{1064, 500}, false},
		{"fragment in a room of 64", []int{1052, 500}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n := newNetwork(t, 34, Config{})
			n.open(t, false)
			a := n.peers[0].assoc
			first := a.nextTSN
			// A zero window holds the messages until both are queued.
			a.send = func([]byte) {}
			sendAll(t, a, n.now, 1)
			sack(t, a, n.now, first-1, uint32(a.Stats().Flight))
			sendAll(t, a, n.now, c.sizes...)
			var packets [][]uint32
			a.send = func(p []byte) { packets = append(packets, dataTSNs(p)) }
			sack(t, a, n.now, first, 1<<20)
			if together := slices.Equal(packets[0], []uint32{first + 1, first + 2}); together != c.together {
				t.Fatalf("first packet carries %v, together: %v, want %v", packets[0], together, c.together)
			}
		})
	}
}

// TestFastRetransmitOnePacketFree covers two chunks missed three times at
// once while the window is full: one packet of them goes without regard
// to the window, the other waits for room in it.
func TestFastRetransmitOnePacketFree(t *testing.T) {
	n := newNetwork(t, 35, Config{})
	n.open(t, false)
	a := n.peers[0].assoc
	first := a.nextTSN
	sendAll(t, a, n.now, slices.Repeat([]int{1100}, 10)...)
	sent := recordData(a)
	for k := uint16(1); k <= 3; k++ {
		sack(t, a, n.now, first-1, 1<<20, 3, 2+k)
	}
	if !slices.Equal(*sent, []uint32{first}) {
		t.Fatalf("sent %v again, want only %d before the window has room", *sent, first)
	}
}

// TestRetransmissionsBeforeNewData covers data queued while chunks wait to
// go again: none of it goes until they all went, though a packet of them
// has room left (RFC 9260 section 6.1).
func TestRetransmissionsBeforeNewData(t *testing.T) {
	n := newNetwork(t, 36, Config{})
	n.open(t, false)
	a := n.peers[0].assoc
	first := a.nextTSN
	// Eleven chunks fill the window; the small message waits.
	sendAll(t, a, n.now, append(slices.Repeat([]int{1100}, 11), 10)...)
	sent := recordData(a)
	for a.Stats().Timeouts == 0 {
		if err := a.HandleTimeout(a.Deadline()); err != nil {
			t.Fatal(err)
		}
	}
	if slices.Contains(*sent, first+11) {
		t.Fatalf("sent %v after a timeout: new data %d went ahead of retransmissions", *sent, first+11)
	}
}

// TestControlChunksOverSeveralPackets covers control chunks queued at once
// that one packet cannot carry: they all go, each packet within the size.
func TestControlChunksOverSeveralPackets(t *testing.T) {
	n := newNetwork(t, 37, Config{})
	n.open(t, false)
	b := n.peers[1].assoc
	var echoes, packets int
	b.send = func(p []byte) {
		if len(p) > b.cfg.MaxPacketSize {
			t.Errorf("packet of %d bytes over the limit of %d", len(p), b.cfg.MaxPacketSize)
		}
		packets++
		for _, c := range packetChunks(p) {
			if c.typ == chunkHeartbeatAck {
				echoes++
			}
		}
	}
	var p packetBuilder
	p.start(header{srcPort: 5000, dstPort: 5000, tag: b.localTag})
	p.addChunk(chunkHeartbeat, 0, make([]byte, 1000))
	p.addChunk(chunkHeartbeat, 0, make([]byte, 1000))
	if err := b.HandlePacket(p.finish(), n.now); err != nil {
		t.Fatal(err)
	}
	if echoes != 2 || packets != 2 {
		t.Fatalf("%d of 2 HEARTBEATs echoed in %d packets", echoes, packets)
	}
}

// TestDelayedSACKRidesWithData covers a SACK delayed for data received:
// the next packet of data sent carries it.
func TestDelayedSACKRidesWithData(t *testing.T) {
	n := newNetwork(t, 38, Config{})
	n.open(t, false)
	b := n.peers[1].assoc
	b.send = func([]byte) {}
	if err := inject(b, n.now, chunkData, flagBegin|flagEnd, dataValue(b.peerCum+1, []byte{1})); err != nil {
		t.Fatal(err)
	}
	var withData []bool
	b.send = func(p []byte) { withData = append(withData, hasChunk(p, chunkSack) && len(dataTSNs(p)) > 0) }
	sendAll(t, b, n.now, 10)
	if !slices.Equal(withData, []bool{true}) {
		t.Fatalf("packets carrying the SACK with the data: %v, want one", withData)
	}
}

var start = time.Unix(1_700_000_000, 0)

// prSCTP is the parameter of an INIT ACK that offers FORWARD TSN.
var prSCTP = appendParam(nil, paramForwardTSN, nil)

// timeout runs the retransmission timer of an association to its expiry,
// and returns when that was.
func timeout(t *testing.T, a *Association) time.Time {
	t.Helper()
	at := a.Deadline()
	for n := a.Stats().Timeouts; a.Stats().Timeouts == n; {
		if err := a.HandleTimeout(a.Deadline()); err != nil {
			t.Fatal(err)
		}
	}
	return at
}

// TestWindowGrowsWhenFullyUsed covers slow start: a SACK that moves the
// cumulative TSN grows the window by the bytes it acknowledges when the
// window was fully used, a packet less than it or more in flight, and not
// otherwise (RFC 9260 section 7.2.1).
func TestWindowGrowsWhenFullyUsed(t *testing.T) {
	for last, grows := range map[int]bool{128: true, 127: false} {
		a := connectTo(t, start)
		first := a.nextTSN
		cwnd := a.Stats().Cwnd
		// Nine chunks of 1147 bytes and one of 16 more than last.
		sendAll(t, a, start, append(slices.Repeat([]int{1131}, 9), last)...)
		if full := a.Stats().Flight >= cwnd-a.cfg.MaxPacketSize; full != grows {
			t.Fatalf("%d bytes in flight under a window of %d", a.Stats().Flight, cwnd)
		}
		sack(t, a, start, first, 1<<20)
		if grew := a.Stats().Cwnd == cwnd+1131; grew != grows || !grew && a.Stats().Cwnd != cwnd {
			t.Fatalf("window %d after a chunk of 1131 bytes acknowledged, was %d; growth wanted: %v", a.Stats().Cwnd, cwnd, grows)
		}
	}
}

// TestCongestionAvoidance covers the window past the slow start threshold:
// it grows by a packet once a window of bytes was acknowledged, and not
// before (RFC 9260 section 7.2.2).
func TestCongestionAvoidance(t *testing.T) {
	a := connectTo(t, start)
	first := a.nextTSN
	mtu := a.cfg.MaxPacketSize
	// A loss found by miss indications halves the window; the SACK that ends
	// the recovery grows it by a packet, past the threshold.
	sendAll(t, a, start, slices.Repeat([]int{1131}, 10)...)
	for k := uint16(2); k <= 4; k++ {
		sack(t, a, start, first-1, 1<<20, 2, k)
	}
	sack(t, a, start, first+9, 1<<20)
	cwnd := a.Stats().Cwnd
	if cwnd != initialCwndMTUs*mtu/2+mtu {
		t.Fatalf("window %d after the recovery, want %d", cwnd, initialCwndMTUs*mtu/2+mtu)
	}
	// Rounds that keep the window fully used: 6786 bytes acknowledged, then
	// 192 more make the window.
	sendAll(t, a, start, slices.Repeat([]int{1131}, 6)...)
	sack(t, a, start, first+15, 1<<20)
	if a.Stats().Cwnd != cwnd {
		t.Fatalf("window %d after %d bytes acknowledged, want %d", a.Stats().Cwnd, 6*1131, cwnd)
	}
	sendAll(t, a, start, append([]int{192}, slices.Repeat([]int{1131}, 5)...)...)
	sack(t, a, start, first+16, 1<<20)
	if a.Stats().Cwnd != cwnd+mtu {
		t.Fatalf("window %d after a window of bytes acknowledged, want %d", a.Stats().Cwnd, cwnd+mtu)
	}
}

// TestWindowGrowsByBytesNewlyAcknowledged covers slow start after a
// timeout, the window fully used: it grows by the bytes a SACK moving the
// cumulative TSN newly acknowledges, those of its gap blocks too, and not
// by those acknowledged before.
func TestWindowGrowsByBytesNewlyAcknowledged(t *testing.T) {
	a := connectTo(t, start)
	first := a.nextTSN
	sendAll(t, a, start, 100, 200, 300, 400)
	at := timeout(t, a)
	cwnd := a.Stats().Cwnd
	sack(t, a, at, first-1, 1<<20, 2, 2)
	if a.Stats().Cwnd != cwnd {
		t.Fatalf("window %d after a gap ack alone, want %d", a.Stats().Cwnd, cwnd)
	}
	sack(t, a, at, first, 1<<20, 1, 3)
	if a.Stats().Cwnd != cwnd+800 {
		t.Fatalf("window %d after 800 bytes newly acknowledged, want %d", a.Stats().Cwnd, cwnd+800)
	}
	sendAll(t, a, at, 900)
	sack(t, a, at, first+3, 1<<20)
	if a.Stats().Cwnd != cwnd+800 {
		t.Fatalf("window %d after a SACK of bytes acknowledged before, want %d", a.Stats().Cwnd, cwnd+800)
	}
}

// TestWindowIgnoresChunksGivenUp covers a SACK that acknowledges a chunk
// given up, out of flight: its bytes do not grow the window.
func TestWindowIgnoresChunksGivenUp(t *testing.T) {
	a := connectTo(t, start, prSCTP)
	first := a.nextTSN
	sendAll(t, a, start, 1)
	if err := a.Send(2, PPIDBinary, make([]byte, 100), SendOptions{Partial: true}, start); err != nil {
		t.Fatal(err)
	}
	sendAll(t, a, start, 200)
	at := timeout(t, a)
	sack(t, a, at, first, 1<<20, 1, 2)
	if want := a.cfg.MaxPacketSize + 201; a.Stats().Cwnd != want {
		t.Fatalf("window %d, want %d", a.Stats().Cwnd, want)
	}
}

// TestRTTFromCumulativeAck covers the round trip time: the first chunk
// sent is timed, and the SACK that acknowledges it gives the one sample
// (RFC 9260 section 6.3.1).
func TestRTTFromCumulativeAck(t *testing.T) {
	a := connectTo(t, start)
	first := a.nextTSN
	sendAll(t, a, start, 1, 1)
	sack(t, a, start.Add(time.Second), first, 1<<20)
	sack(t, a, start.Add(2*time.Second), first+1, 1<<20)
	if st := a.Stats(); a.RTT() != time.Second || st.RTT != time.Second || st.RTO != 3*time.Second {
		t.Fatalf("RTT %v, RTO %v; want 1s and 3s", st.RTT, st.RTO)
	}
}

// TestRTTFromGapAck covers a timed chunk acknowledged by a gap block: it
// gives a sample, and the chunk after it none.
func TestRTTFromGapAck(t *testing.T) {
	a := connectTo(t, start)
	first := a.nextTSN
	sendAll(t, a, start, 1)
	at := timeout(t, a).Add(time.Second)
	sendAll(t, a, at, 1, 1)
	sack(t, a, at.Add(2*time.Second), first-1, 1<<20, 2, 3)
	if st := a.Stats(); st.RTT != 2*time.Second || st.RTO != 6*time.Second {
		t.Fatalf("RTT %v, RTO %v; want 2s and 6s", st.RTT, st.RTO)
	}
}

// TestRTTKarn covers Karn's rule: a timed chunk sent again gives no
// sample, and the chunk sent next is timed instead.
func TestRTTKarn(t *testing.T) {
	a := connectTo(t, start)
	first := a.nextTSN
	sendAll(t, a, start, 1, 1, 1, 1)
	for k := uint16(2); k <= 4; k++ {
		sack(t, a, start, first-1, 1<<20, 2, k)
	}
	at := start.Add(time.Second)
	sack(t, a, at, first+3, 1<<20)
	if a.RTT() != 0 {
		t.Fatalf("RTT %v from a chunk sent twice", a.RTT())
	}
	sendAll(t, a, at, 1)
	sack(t, a, at.Add(time.Second), first+4, 1<<20)
	if a.RTT() != time.Second {
		t.Fatalf("RTT %v from the chunk sent next, want 1s", a.RTT())
	}
}

// TestRTTKeptWhenOtherChunkLost covers a chunk sent again by fast
// retransmit while another is timed: the timed one still gives its sample.
func TestRTTKeptWhenOtherChunkLost(t *testing.T) {
	a := connectTo(t, start)
	first := a.nextTSN
	// P, Q, R and four more: P is timed, P and R are lost. P is missed three
	// times first and goes again; then the chunk after the seven is timed.
	sendAll(t, a, start, 1, 1, 1, 1, 1, 1, 1)
	sack(t, a, start, first-1, 1<<20, 2, 2)
	sack(t, a, start, first-1, 1<<20, 2, 2, 4, 4)
	sack(t, a, start, first-1, 1<<20, 2, 2, 4, 5)
	sent := recordData(a)
	sendAll(t, a, start, 1)
	// R is missed a third time and goes again.
	sack(t, a, start, first-1, 1<<20, 2, 2, 4, 6)
	if !slices.Equal(*sent, []uint32{first + 7, first + 2}) {
		t.Fatalf("sent %v, want %d new then %d again", *sent, first+7, first+2)
	}
	sack(t, a, start.Add(time.Second), first+7, 1<<20)
	if a.RTT() != time.Second {
		t.Fatalf("RTT %v, want the 1s of the chunk timed", a.RTT())
	}
}

// TestErrorCountResets covers the count of timeouts in a row that fails
// the association: a SACK that acknowledges data anew, a byte by a gap
// block too, starts it over; one that acknowledges nothing new does not.
func TestErrorCountResets(t *testing.T) {
	a := connectWith(t, Config{MaxRetransmissions: 2}, start)
	first := a.nextTSN
	sendAll(t, a, start, 1, 1)
	expire := func(want error) {
		t.Helper()
		if err := a.HandleTimeout(a.Deadline()); !errors.Is(err, want) {
			t.Fatalf("timeout %d: %v, want %v", a.Stats().Timeouts, err, want)
		}
	}
	expire(nil)
	expire(nil)
	sack(t, a, a.Deadline(), first-1, 1<<20, 2, 2)
	expire(nil)
	expire(nil)
	sack(t, a, a.Deadline(), first-1, 1<<20, 2, 2)
	expire(ErrTimeout)
	if a.Stats().Timeouts != 4 {
		t.Fatalf("failed after %d timeouts, want 4", a.Stats().Timeouts)
	}
}

// TestTimeoutThresholdFloor covers the slow start threshold after a second
// timeout: it does not go under four packets, so the window grows by slow
// start again (RFC 9260 section 7.2.3).
func TestTimeoutThresholdFloor(t *testing.T) {
	a := connectTo(t, start)
	first := a.nextTSN
	sendAll(t, a, start, 1000)
	timeout(t, a)
	at := timeout(t, a)
	sack(t, a, at, first, 1<<20)
	if want := a.cfg.MaxPacketSize + 1000; a.Stats().Cwnd != want {
		t.Fatalf("window %d, want %d", a.Stats().Cwnd, want)
	}
}

// TestTimeoutEndsFastRecovery covers a timeout during fast recovery: it
// ends the recovery, and the window grows again by slow start.
func TestTimeoutEndsFastRecovery(t *testing.T) {
	a := connectTo(t, start)
	first := a.nextTSN
	sendAll(t, a, start, slices.Repeat([]int{1}, 10)...)
	for k := uint16(2); k <= 4; k++ {
		sack(t, a, start, first-1, 1<<20, 2, k)
	}
	at := timeout(t, a)
	sack(t, a, at, first, 1<<20)
	if want := a.cfg.MaxPacketSize + 1; a.Stats().Cwnd != want {
		t.Fatalf("window %d, want %d", a.Stats().Cwnd, want)
	}
}

// TestTimeoutSkipsAcknowledged covers a timeout with a chunk acknowledged
// by a gap block: only the one missing goes again.
func TestTimeoutSkipsAcknowledged(t *testing.T) {
	a := connectTo(t, start)
	first := a.nextTSN
	sendAll(t, a, start, 1, 1)
	sack(t, a, start, first-1, 1<<20, 2, 2)
	sent := recordData(a)
	timeout(t, a)
	if !slices.Equal(*sent, []uint32{first}) {
		t.Fatalf("sent %v again, want %d alone", *sent, first)
	}
}

// TestMaxRetransmitsCounted covers a partially reliable message sent at
// most MaxRetransmits times again: then it is given up and skipped.
func TestMaxRetransmitsCounted(t *testing.T) {
	a := connectTo(t, start, prSCTP)
	sent := recordPackets(a)
	if err := a.Send(1, PPIDBinary, []byte("lost"), SendOptions{Partial: true, MaxRetransmits: 2}, start); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		timeout(t, a)
	}
	if want := []byte{chunkData, chunkData, chunkData, chunkForwardTSN}; !slices.Equal(chunksSent(*sent), want) {
		t.Fatalf("sent %v, want %v", chunksSent(*sent), want)
	}
}

// TestRetransmitsCounted covers Stats.Retransmits: a chunk marked to go
// again counts once until it goes.
func TestRetransmitsCounted(t *testing.T) {
	a := connectTo(t, start)
	sendAll(t, a, start, 1100, 1100, 1100)
	var counts []uint64
	for range 2 {
		timeout(t, a)
		counts = append(counts, a.Stats().Retransmits)
	}
	// The window of one packet left after the first timeout lets two go.
	if !slices.Equal(counts, []uint64{3, 5}) {
		t.Fatalf("retransmissions counted %v, want [3 5]", counts)
	}
}

// TestAbandonedMessageNotSentAgain covers a partially reliable message of
// two chunks, the first missed three times: both are given up, neither
// goes again nor stays in flight, and a FORWARD TSN skips them.
func TestAbandonedMessageNotSentAgain(t *testing.T) {
	a := connectTo(t, start, prSCTP)
	first := a.nextTSN
	if err := a.Send(2, PPIDBinary, payload(1, 2000), SendOptions{Unordered: true, Partial: true}, start); err != nil {
		t.Fatal(err)
	}
	sendAll(t, a, start, 1, 1, 1)
	sent := recordPackets(a)
	for k := uint16(3); k <= 5; k++ {
		sack(t, a, start, first-1, 1<<20, 3, k)
	}
	if !slices.Equal(chunksSent(*sent), []byte{chunkForwardTSN}) || a.Stats().Flight != 0 {
		t.Fatalf("sent %v, %d bytes in flight; want a FORWARD TSN alone and none", chunksSent(*sent), a.Stats().Flight)
	}
	sack(t, a, start, first+4, 1<<20)
	if st := a.Stats(); st.Flight != 0 || st.Buffered != 0 {
		t.Fatalf("%d bytes in flight, %d buffered once all is acknowledged", st.Flight, st.Buffered)
	}
}

// TestForwardTSNWhenSkipPossible covers a chunk given up behind one lost
// that is not: the FORWARD TSN goes with the SACK that acknowledges the
// one before it, and not again with the SACKs after while nothing changes.
func TestForwardTSNWhenSkipPossible(t *testing.T) {
	a := connectTo(t, start, prSCTP)
	first := a.nextTSN
	sendAll(t, a, start, 1)
	if err := a.Send(2, PPIDBinary, []byte{2}, SendOptions{Partial: true}, start); err != nil {
		t.Fatal(err)
	}
	at := timeout(t, a)
	sent := recordPackets(a)
	sack(t, a, at, first, 1<<20)
	sack(t, a, at, first, 1<<20)
	if !slices.Equal(chunksSent(*sent), []byte{chunkForwardTSN}) {
		t.Fatalf("sent %v, want one FORWARD TSN", chunksSent(*sent))
	}
}

// TestForwardTSNStreams covers the streams a FORWARD TSN names: those of
// the ordered messages given up, each with the last SSN, in order; not
// those of unordered ones (RFC 3758 section 3.5).
func TestForwardTSNStreams(t *testing.T) {
	a := connectTo(t, start, prSCTP)
	first := a.nextTSN
	opts := SendOptions{Partial: true}
	unordered := SendOptions{Partial: true, Unordered: true}
	for _, m := range []struct {
		stream uint16
		opts   SendOptions
	}{{3, opts}, {3, opts}, {4, unordered}, {2, opts}, {3, opts}} {
		if err := a.Send(m.stream, PPIDBinary, []byte{1}, m.opts, start); err != nil {
			t.Fatal(err)
		}
	}
	var value []byte
	a.send = func(p []byte) {
		for _, c := range packetChunks(p) {
			if c.typ == chunkForwardTSN {
				value = bytes.Clone(c.value)
			}
		}
	}
	timeout(t, a)
	if want := forwardTSN(first+4, 2, 0, 3, 2); !bytes.Equal(value, want) {
		t.Fatalf("FORWARD TSN % x, want % x", value, want)
	}
}

// TestForwardTSNFitsPacket covers a FORWARD TSN that would name more
// streams than a packet takes: it skips the chunks up to the last whose
// streams fit, and the SACK of it brings one for the rest (RFC 3758
// section 3.5, rule C4).
func TestForwardTSNFitsPacket(t *testing.T) {
	a := connectTo(t, start, prSCTP)
	first := a.nextTSN
	// A packet of 1163 bytes takes a FORWARD TSN of 285 streams.
	var pairs []uint16
	for s := range uint16(300) {
		if err := a.Send(s, PPIDBinary, []byte{1}, SendOptions{Partial: true}, start); err != nil {
			t.Fatal(err)
		}
		pairs = append(pairs, s, 0)
	}
	var values [][]byte
	a.send = func(p []byte) {
		for _, c := range packetChunks(p) {
			if c.typ == chunkForwardTSN {
				values = append(values, bytes.Clone(c.value))
			}
		}
	}
	at := timeout(t, a)
	sack(t, a, at, first+284, 1<<20)
	want := [][]byte{forwardTSN(first+284, pairs[:2*285]...), forwardTSN(first+299, pairs[2*285:]...)}
	if !slices.EqualFunc(values, want, bytes.Equal) {
		t.Fatalf("sent %d FORWARD TSNs, want two: to TSN %d naming 285 streams, then to %d naming 15", len(values), first+284, first+299)
	}
}

// TestSendBufferBoundary covers a message that fills the send buffer
// exactly: it is accepted, and the next byte is not.
func TestSendBufferBoundary(t *testing.T) {
	a := connectWith(t, Config{SendBuffer: 1000}, start)
	sendAll(t, a, start, 600, 400)
	if err := a.Send(1, PPIDBinary, []byte{1}, SendOptions{}, start); !errors.Is(err, ErrBufferFull) {
		t.Fatalf("a byte past the buffer: %v, want ErrBufferFull", err)
	}
}

// TestSendCopiesMessage covers Send: the message goes as it was at the
// call, again too, though the caller changed it after.
func TestSendCopiesMessage(t *testing.T) {
	a := connectTo(t, start)
	data := []byte("hello")
	if err := a.Send(1, PPIDBinary, data, SendOptions{}, start); err != nil {
		t.Fatal(err)
	}
	copy(data, "world")
	var again []byte
	a.send = func(p []byte) { again = bytes.Clone(packetChunks(p)[0].value[12:]) }
	timeout(t, a)
	if string(again) != "hello" {
		t.Fatalf("sent %q again, want %q", again, "hello")
	}
}

// TestHandleTimeoutRunsEveryTimerDue covers timers expiring at once: one
// call runs both, and sends no SACK, none being due.
func TestHandleTimeoutRunsEveryTimerDue(t *testing.T) {
	a := connectTo(t, start, appendParam(nil, paramSupportedExtension, []byte{chunkReconfig}))
	sendAll(t, a, start, 1)
	if err := a.ResetStreams([]uint16{2}, start); err != nil {
		t.Fatal(err)
	}
	sent := recordPackets(a)
	if err := a.HandleTimeout(a.Deadline()); err != nil {
		t.Fatal(err)
	}
	if want := []byte{chunkData, chunkReconfig}; !slices.Equal(chunksSent(*sent), want) {
		t.Fatalf("sent %v, want %v", chunksSent(*sent), want)
	}
}

// TestSACKChecked covers a SACK shorter than its fixed fields and one
// that acknowledges data never sent: the association fails with an ABORT.
func TestSACKChecked(t *testing.T) {
	for _, value := range []func(a *Association) []byte{
		func(a *Association) []byte { return appendSack(nil, a.nextTSN-1, 1<<20, nil, nil)[:11] },
		func(a *Association) []byte { return appendSack(nil, a.nextTSN, 1<<20, nil, nil) },
	} {
		a := connectTo(t, start)
		sendAll(t, a, start, 1)
		sent := recordPackets(a)
		if err := inject(a, start, chunkSack, 0, value(a)); !errors.Is(err, errPacket) || !slices.Equal(chunksSent(*sent), []byte{chunkAbort}) {
			t.Fatalf("failed with %v, sent %v; want a malformed packet and an ABORT", err, chunksSent(*sent))
		}
	}
}

// TestSACKWithoutProgressKeepsTimer covers a SACK that neither moves the
// cumulative TSN nor withdraws a gap ack: the retransmission timer runs on
// (RFC 9260 section 6.3.2).
func TestSACKWithoutProgressKeepsTimer(t *testing.T) {
	a := connectTo(t, start)
	first := a.nextTSN
	sendAll(t, a, start, 1, 1)
	deadline := a.Deadline()
	sack(t, a, start.Add(100*time.Millisecond), first-1, 1<<20, 2, 2)
	if !a.Deadline().Equal(deadline) {
		t.Fatalf("timer moved by %v", a.Deadline().Sub(deadline))
	}
}

// TestRenegedChunkMissedAfresh covers a chunk the peer reneged on: its
// count of misses starts over, so it goes again after three new ones.
func TestRenegedChunkMissedAfresh(t *testing.T) {
	a := connectTo(t, start)
	first := a.nextTSN
	sendAll(t, a, start, 1, 1, 1, 1, 1, 1)
	sack(t, a, start, first-1, 1<<20, 2, 2)
	sack(t, a, start, first-1, 1<<20)
	sent := recordData(a)
	for k := uint16(3); k <= 4; k++ {
		sack(t, a, start, first-1, 1<<20, 3, k)
	}
	if !slices.Equal(*sent, []uint32{first}) {
		t.Fatalf("sent %v after two misses of the chunk reneged on, want %d alone", *sent, first)
	}
	sack(t, a, start, first-1, 1<<20, 3, 5)
	if !slices.Equal(*sent, []uint32{first, first + 1}) {
		t.Fatalf("sent %v after three, want %d too", *sent, first+1)
	}
}

// TestFastRecoveryMisses covers miss indications in fast recovery: a SACK
// that moves the cumulative TSN counts one for every TSN it reports
// missing; one that does not counts by the highest TSN newly acknowledged
// alone (RFC 9260 section 7.2.4).
func TestFastRecoveryMisses(t *testing.T) {
	a := connectTo(t, start)
	first := a.nextTSN
	sendAll(t, a, start, slices.Repeat([]int{1}, 10)...)
	// T1 is lost and missed three times; T3 and T5 are lost too.
	sack(t, a, start, first-1, 1<<20, 2, 2)
	sack(t, a, start, first-1, 1<<20, 2, 2, 4, 4)
	sack(t, a, start, first-1, 1<<20, 2, 2, 4, 4, 6, 6)
	sent := recordData(a)
	// T3 arrives late: T5, past it, is not missed. Then T7 and T8 arrive.
	sack(t, a, start, first-1, 1<<20, 2, 4, 6, 6)
	sack(t, a, start, first-1, 1<<20, 2, 4, 6, 7)
	if len(*sent) != 0 {
		t.Fatalf("sent %v with T5 missed twice", *sent)
	}
	// The cumulative TSN moves to T4: T5, still reported missing, is
	// missed a third time.
	sack(t, a, start, first+3, 1<<20, 2, 4)
	if !slices.Equal(*sent, []uint32{first + 4}) {
		t.Fatalf("sent %v, want T5 again", *sent)
	}
}
