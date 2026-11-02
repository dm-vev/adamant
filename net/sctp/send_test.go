package sctp

import (
	"bytes"
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
// waits for the window past its lifetime: it is dropped without a byte of
// it sent, and its room in the send buffer comes back.
func TestTimedMessageExpiresUnsent(t *testing.T) {
	n := newNetwork(t, 33, Config{})
	n.open(t, false)
	a := n.peers[0].assoc
	first := a.nextTSN
	// Eleven messages fill the window, the twelfth waits.
	sendAll(t, a, n.now, slices.Repeat([]int{1100}, 12)...)
	opts := SendOptions{Partial: true, MaxRetransmits: 5, Lifetime: 100 * time.Millisecond}
	if err := a.Send(2, PPIDBinary, []byte("late"), opts, n.now); err != nil {
		t.Fatal(err)
	}
	sent := recordData(a)
	// Everything sent is acknowledged once the lifetime passed.
	last := first + uint32(len(a.outstanding)) - 1
	sack(t, a, n.now.Add(time.Second), last, 1<<20)
	if slices.Contains(*sent, last+2) || a.Buffered() != 1100 {
		t.Fatalf("sent %v after the lifetime, %d bytes buffered; want the expired message dropped unsent", *sent, a.Buffered())
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
