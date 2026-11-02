package sctp

import (
	"encoding/binary"
	"slices"
	"testing"
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
// after the delay: a chunk out of order, one that closes the last gap and
// one with the I bit go at once (RFC 9260 section 6.7, RFC 7053); a lone
// chunk in order waits.
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
