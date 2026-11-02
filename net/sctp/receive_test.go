package sctp

import (
	"bytes"
	"encoding/binary"
	"slices"
	"testing"
	"time"
)

// dataValue returns the value of a DATA chunk at tsn on stream 1.
func dataValue(tsn uint32, data []byte) []byte {
	head := make([]byte, 12, 12+len(data))
	binary.BigEndian.PutUint32(head, tsn)
	binary.BigEndian.PutUint16(head[4:], 1)
	return append(head, data...)
}

// sackIn returns the SACK of a packet, if it carries one.
func sackIn(p []byte) (sackChunk, bool) {
	for _, c := range packetChunks(p) {
		if c.typ == chunkSack {
			return parseSack(c.value, nil)
		}
	}
	return sackChunk{}, false
}

// TestSACKTiming covers when a receiver acknowledges at once rather than
// after the delay: a chunk out of order, one that closes the last gap, a
// duplicate and one with the I bit go at once (RFC 9260 section 6.7, RFC
// 7053); a lone chunk in order waits.
func TestSACKTiming(t *testing.T) {
	cases := []struct {
		name string
		// offsets are the TSNs sent after the cumulative one, the last
		// being the chunk checked.
		offsets []uint32
		flags   byte
		atOnce  bool
	}{
		{"in order", []uint32{1}, 0, false},
		{"out of order", []uint32{2}, 0, true},
		{"closing the last gap", []uint32{2, 1}, 0, true},
		{"immediate", []uint32{1}, flagImmediate, true},
		{"duplicate", []uint32{1, 1}, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n := newNetwork(t, 25, Config{})
			n.open(t, false)
			b := n.peers[1].assoc
			cum := b.peerCum
			acked := false
			b.send = func(p []byte) { acked = acked || hasChunk(p, chunkSack) }
			for _, off := range c.offsets {
				acked = false
				if err := inject(b, n.now, chunkData, flagBegin|flagEnd|c.flags, dataValue(cum+off, []byte{1})); err != nil {
					t.Fatal(err)
				}
			}
			if acked != c.atOnce {
				t.Fatalf("acknowledged at once: %v, want %v", acked, c.atOnce)
			}
		})
	}
}

// TestOutOfOrderWithinWindow covers chunks out of order against the
// receive window: they are kept while they fit it, the one past it is
// dropped for the peer to send again, and the SACK tells which.
func TestOutOfOrderWithinWindow(t *testing.T) {
	n := newNetwork(t, 26, Config{ReceiveWindow: 4000})
	n.open(t, false)
	b := n.peers[1].assoc
	cum := b.peerCum
	var last sackChunk
	b.send = func(p []byte) {
		if s, ok := sackIn(p); ok {
			last = s
		}
	}
	// Fragments of messages not ended, in chunks of 1000 bytes, are held
	// until the gap closes.
	for i := range uint32(5) {
		if err := inject(b, n.now, chunkData, flagBegin, dataValue(cum+2+i, make([]byte, 1000-dataHeaderSize))); err != nil {
			t.Fatal(err)
		}
	}
	// A whole message past the window is dropped too, not delivered: the
	// peer sends it again.
	if err := inject(b, n.now, chunkData, flagBegin|flagEnd|flagUnordered, dataValue(cum+7, []byte{1})); err != nil {
		t.Fatal(err)
	}
	if last.cumTSN != cum || !slices.Equal(last.gaps, []uint16{2, 5}) {
		t.Fatalf("SACK of %d with gaps %v, want %d with [2 5]", last.cumTSN, last.gaps, cum)
	}
	if got := len(n.peers[1].received); got != 0 {
		t.Fatalf("delivered %d messages dropped for lack of room", got)
	}
}

// TestUnorderedMessagesOverLossyNetwork covers unordered messages of up to
// six fragments over a path that loses, duplicates and reorders packets:
// each arrives once and whole.
func TestUnorderedMessagesOverLossyNetwork(t *testing.T) {
	for seed := range uint64(4) {
		n := newNetwork(t, 50+seed, Config{})
		n.open(t, false)
		n.loss, n.dup, n.shuffle = 0.1, 0.05, true
		a, b := n.peers[0], n.peers[1]
		const count = 100
		// The sizes differ, and tell the messages apart.
		size := func(i int) int { return 1 + i*1733%6000 }
		for i := range count {
			if err := a.assoc.Send(uint16(i%3), PPIDBinary, payload(i, size(i)), SendOptions{Unordered: true}, n.now); err != nil {
				t.Fatal(err)
			}
		}
		n.run(10*time.Minute, func() bool { return len(b.received) == count })
		got := map[int]bool{}
		for _, m := range b.received {
			i := -1
			for j := range count {
				if size(j) == len(m.data) {
					i = j
				}
			}
			if i < 0 || got[i] || m.stream != uint16(i%3) || !bytes.Equal(m.data, payload(i, size(i))) {
				t.Fatalf("seed %d: message of %d bytes on stream %d unknown, repeated or corrupt", seed, len(m.data), m.stream)
			}
			got[i] = true
		}
		if len(got) != count || b.assoc.held != 0 {
			t.Fatalf("seed %d: received %d of %d messages, %d bytes still held", seed, len(got), count, b.assoc.held)
		}
	}
}

// receiver returns the second association of a network, open, recording
// the SACKs it sends, with the TSN it acknowledged cumulatively.
func receiver(t *testing.T, seed uint64, cfg Config) (*network, *Association, *[]sackChunk, uint32) {
	n := newNetwork(t, seed, cfg)
	n.open(t, false)
	b := n.peers[1].assoc
	var sacks []sackChunk
	b.send = func(p []byte) {
		for _, c := range packetChunks(p) {
			if c.typ == chunkSack {
				s, _ := parseSack(c.value, nil)
				s.dups = make([]uint32, binary.BigEndian.Uint16(c.value[10:]))
				sacks = append(sacks, s)
			}
		}
	}
	return n, b, &sacks, b.peerCum
}

// forwardTSN returns the value of a FORWARD TSN chunk, with the streams
// and SSNs given in pairs.
func forwardTSN(cum uint32, pairs ...uint16) []byte {
	v := binary.BigEndian.AppendUint32(nil, cum)
	for _, x := range pairs {
		v = binary.BigEndian.AppendUint16(v, x)
	}
	return v
}

// TestDuplicatesReported covers chunks received twice: the SACK reports
// them, at most 16.
func TestDuplicatesReported(t *testing.T) {
	n, b, sacks, cum := receiver(t, 80, Config{})
	var p packetBuilder
	p.start(header{srcPort: 5000, dstPort: 5000, tag: b.localTag})
	for range 20 {
		p.addChunk(chunkData, flagBegin|flagEnd, dataValue(cum, []byte{1}))
	}
	if err := b.HandlePacket(p.finish(), n.now); err != nil {
		t.Fatal(err)
	}
	if len(*sacks) != 1 || len((*sacks)[0].dups) != maxDuplicates {
		t.Fatalf("SACKs %v, want one reporting %d duplicates", *sacks, maxDuplicates)
	}
}

// TestForwardTSNChecked covers the FORWARD TSNs a receiver acts on: one of
// a whole number of stream entries that moves the cumulative TSN ahead, by
// at most the bound on TSNs above it.
func TestForwardTSNChecked(t *testing.T) {
	cases := []struct {
		name  string
		value func(cum uint32) []byte
		moves bool
	}{
		{"ahead", func(cum uint32) []byte { return forwardTSN(cum + 1) }, true},
		{"at the bound", func(cum uint32) []byte { return forwardTSN(cum + tsnWindow) }, true},
		{"past the bound", func(cum uint32) []byte { return forwardTSN(cum + tsnWindow + 1) }, false},
		{"half an entry", func(cum uint32) []byte { return append(forwardTSN(cum+1), 0, 1) }, false},
		{"behind", func(cum uint32) []byte { return forwardTSN(cum - 1) }, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n, b, sacks, cum := receiver(t, 81, Config{})
			if err := inject(b, n.now, chunkForwardTSN, 0, c.value(cum)); err != nil {
				t.Fatal(err)
			}
			if moved := len(*sacks) > 0 && (*sacks)[0].cumTSN != cum; moved != c.moves {
				t.Fatalf("SACKs %v after the FORWARD TSN; cumulative TSN moved: %v, want %v", *sacks, moved, c.moves)
			}
		})
	}
}

// TestForwardTSNSkipsOrderedMessages covers a FORWARD TSN that skips
// ordered messages: the next message held takes its turn, a message
// skipped held in part is dropped, not delivered in part, and one past a
// message still missing waits for it (RFC 3758 section 3.6).
func TestForwardTSNSkipsOrderedMessages(t *testing.T) {
	n, b, _, cum := receiver(t, 82, Config{})
	data := func(stream uint16, tsn uint32, ssn uint16, flags byte) {
		t.Helper()
		v := dataOn(stream, tsn, []byte{byte(ssn)})
		binary.BigEndian.PutUint16(v[6:], ssn)
		if err := inject(b, n.now, chunkData, flags, v); err != nil {
			t.Fatal(err)
		}
	}
	// SSN 0 of stream 2 arrived without its first fragment, and SSN 0 of
	// stream 1 was lost: the sender gave them up. SSN 1 of stream 1
	// arrived; SSN 1 of stream 2 was lost, SSN 2 arrived.
	data(2, cum+2, 0, 0)
	data(2, cum+3, 0, flagEnd)
	data(1, cum+5, 1, flagBegin|flagEnd)
	data(2, cum+7, 2, flagBegin|flagEnd)
	// Stream 3 holds nothing; stream 1 comes last.
	if err := inject(b, n.now, chunkForwardTSN, 0, forwardTSN(cum+4, 3, 9, 2, 0, 1, 0)); err != nil {
		t.Fatal(err)
	}
	got := n.peers[1].received
	if len(got) != 1 || got[0].stream != 1 || got[0].data[0] != 1 {
		t.Fatalf("delivered %v, want SSN 1 of stream 1 alone", got)
	}
	data(2, cum+6, 1, flagBegin|flagEnd)
	got = n.peers[1].received
	if len(got) != 3 || got[1].data[0] != 1 || got[2].data[0] != 2 || b.held != 0 {
		t.Fatalf("delivered %v, %d bytes held; want SSNs 1 and 2 of stream 2 next", got, b.held)
	}
}

// TestForwardTSNDropsUnorderedFragments covers the unordered fragments a
// FORWARD TSN skips: they are dropped, those past it kept, and the window
// comes back whole once nothing is held.
func TestForwardTSNDropsUnorderedFragments(t *testing.T) {
	const window = 4000
	n, b, sacks, cum := receiver(t, 83, Config{ReceiveWindow: window})
	data := func(tsn uint32, flags byte) {
		t.Helper()
		if err := inject(b, n.now, chunkData, flags|flagUnordered, dataValue(tsn, []byte{byte(tsn - cum)})); err != nil {
			t.Fatal(err)
		}
	}
	data(cum+1, flagBegin)
	data(cum+4, flagBegin)
	if err := inject(b, n.now, chunkForwardTSN, 0, forwardTSN(cum+2)); err != nil {
		t.Fatal(err)
	}
	data(cum+3, flagBegin|flagEnd)
	data(cum+5, flagEnd)
	if err := inject(b, n.now, chunkForwardTSN, 0, forwardTSN(cum+6)); err != nil {
		t.Fatal(err)
	}
	got := n.peers[1].received
	last := (*sacks)[len(*sacks)-1]
	if len(got) != 2 || !bytes.Equal(got[1].data, []byte{4, 5}) || last.cumTSN != cum+6 || last.window != window {
		t.Fatalf("delivered %v, last SACK %d with a window of %d; want the message of TSNs 4 and 5, and %d with %d", got, last.cumTSN-cum, last.window, 6, window)
	}
}

// TestDelayedSACKOnTimer covers the SACK of a lone chunk in order: it goes
// alone when the delay of 200 ms passes.
func TestDelayedSACKOnTimer(t *testing.T) {
	n, b, _, cum := receiver(t, 84, Config{})
	if err := inject(b, n.now, chunkData, flagBegin|flagEnd, dataValue(cum+1, []byte{1})); err != nil {
		t.Fatal(err)
	}
	sent := recordPackets(b)
	if !b.Deadline().Equal(n.now.Add(sackDelay)) {
		t.Fatalf("next timer in %v, want %v", b.Deadline().Sub(n.now), sackDelay)
	}
	if err := b.HandleTimeout(b.Deadline()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(chunksSent(*sent), []byte{chunkSack}) || !b.Deadline().IsZero() {
		t.Fatalf("sent %v, next timer %v; want a SACK alone and no timer", chunksSent(*sent), b.Deadline())
	}
}
