package sctp

import (
	"encoding/binary"
	"errors"
	"slices"
	"testing"
)

// resetPeer returns the second association of a network, open, recording
// the streams it resets and the reset responses it sends.
func resetPeer(t *testing.T, seed uint64) (*network, *Association, *[][]uint16, *[][2]uint32) {
	var resets [][]uint16
	var responses [][2]uint32
	n := newNetwork(t, seed, Config{})
	n.peers[1].assoc.cfg.OnStreamReset = func(s []uint16) { resets = append(resets, s) }
	n.open(t, false)
	b := n.peers[1].assoc
	b.send = func(p []byte) { responses = append(responses, resetResponses(p)...) }
	return n, b, &resets, &responses
}

// dataOn returns the value of a DATA chunk at tsn on a stream.
func dataOn(stream uint16, tsn uint32, data []byte) []byte {
	v := dataValue(tsn, data)
	binary.BigEndian.PutUint16(v[4:], stream)
	return v
}

// TestResetRequestDeferred covers a reset of streams the peer sends on
// that arrives ahead of the data sent before it: it is answered In
// progress, performed once the data arrives, and answered Performed again
// without being applied twice when it comes again (RFC 6525 sections
// 5.2.1 and 5.2.2).
func TestResetRequestDeferred(t *testing.T) {
	n, b, resets, responses := resetPeer(t, 70)
	seq, cum := b.reset.nextIn, b.peerCum
	request := outgoingReset(seq, cum+1, 1)
	if err := inject(b, n.now, chunkReconfig, 0, request); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*responses, [][2]uint32{{seq, resultInProgress}}) || len(*resets) != 0 {
		t.Fatalf("responses %v, %d resets before the data; want In progress alone", *responses, len(*resets))
	}
	if err := inject(b, n.now, chunkData, flagBegin|flagEnd, dataValue(cum+1, []byte{1})); err != nil {
		t.Fatal(err)
	}
	if err := inject(b, n.now, chunkReconfig, 0, request); err != nil {
		t.Fatal(err)
	}
	want := [][2]uint32{{seq, resultInProgress}, {seq, resultPerformed}, {seq, resultPerformed}}
	if !slices.Equal(*responses, want) || len(*resets) != 1 {
		t.Fatalf("responses %v, %d resets; want %v and one", *responses, len(*resets), want)
	}
}

// TestResetAllStreams covers a reset request that names no stream: every
// stream the peer sent on is reset, and the fragments held on them are
// dropped (RFC 6525 section 5.2.2).
func TestResetAllStreams(t *testing.T) {
	n, b, resets, responses := resetPeer(t, 71)
	seq, cum := b.reset.nextIn, b.peerCum
	for i, stream := range []uint16{2, 1} {
		if err := inject(b, n.now, chunkData, flagBegin, dataOn(stream, cum+1+uint32(i), []byte{1})); err != nil {
			t.Fatal(err)
		}
	}
	if err := inject(b, n.now, chunkReconfig, 0, outgoingReset(seq, cum+2)); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*responses, [][2]uint32{{seq, resultPerformed}}) || len(*resets) != 1 || !slices.Equal((*resets)[0], []uint16{1, 2}) || b.held != 0 {
		t.Fatalf("responses %v, resets %v, %d bytes held; want streams 1 and 2 reset", *responses, *resets, b.held)
	}
}

// TestResetDropsUnorderedFragments covers the unordered fragments of a
// stream reset: they are dropped, and those of other streams kept.
func TestResetDropsUnorderedFragments(t *testing.T) {
	n, b, resets, _ := resetPeer(t, 72)
	seq, cum := b.reset.nextIn, b.peerCum
	data := func(stream uint16, tsn uint32, flags byte) {
		t.Helper()
		if err := inject(b, n.now, chunkData, flags|flagUnordered, dataOn(stream, tsn, []byte{byte(stream)})); err != nil {
			t.Fatal(err)
		}
	}
	// The first fragments of a message on stream 1, in order, and of one on
	// stream 2, out of order; then stream 1 is reset, and the ends of both
	// arrive.
	data(1, cum+1, flagBegin)
	data(2, cum+3, flagBegin)
	if err := inject(b, n.now, chunkReconfig, 0, outgoingReset(seq, cum+1, 1)); err != nil {
		t.Fatal(err)
	}
	data(1, cum+2, flagEnd)
	data(2, cum+4, flagEnd)
	got := n.peers[1].received
	if len(*resets) != 1 || len(got) != 1 || got[0].stream != 2 {
		t.Fatalf("%d resets, delivered %v; want one reset and the message of stream 2 alone", len(*resets), got)
	}
}

// TestResetRequestMalformed covers a reset request shorter than its fixed
// fields: it is ignored.
func TestResetRequestMalformed(t *testing.T) {
	n, b, resets, responses := resetPeer(t, 73)
	for _, size := range []int{4, 11, 13} {
		v := binary.BigEndian.AppendUint32(nil, b.reset.nextIn)
		v = append(v, make([]byte, size-4)...)
		if err := inject(b, n.now, chunkReconfig, 0, appendParam(nil, paramOutgoingReset, v)); err != nil {
			t.Fatal(err)
		}
	}
	if len(*responses) != 0 || len(*resets) != 0 {
		t.Fatalf("answered %v to malformed requests, %d resets", *responses, len(*resets))
	}
}

// TestIncomingResetWithoutStreams covers a request that this side reset
// every stream it sends on, its fixed part alone: it is denied, and one
// shorter than that ignored.
func TestIncomingResetWithoutStreams(t *testing.T) {
	n, b, _, responses := resetPeer(t, 77)
	seq := b.reset.nextIn
	for _, v := range [][]byte{{1, 2, 3}, binary.BigEndian.AppendUint32(nil, seq)} {
		if err := inject(b, n.now, chunkReconfig, 0, appendParam(nil, paramIncomingReset, v)); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(*responses, [][2]uint32{{seq, resultDenied}}) {
		t.Fatalf("responses %v, want one Denied", *responses)
	}
}

// TestResetResponseChecked covers the responses to this side's reset: one
// that answers no request in flight is ignored, a denial ends the request
// without a reset, and a success resets the streams to SSN 0.
func TestResetResponseChecked(t *testing.T) {
	var done [][]uint16
	n := newNetwork(t, 74, Config{})
	n.peers[0].assoc.cfg.OnResetDone = func(s []uint16) { done = append(done, s) }
	n.open(t, false)
	a := n.peers[0].assoc
	first := a.nextTSN
	var ssns []uint16
	a.send = func(p []byte) {
		for _, c := range packetChunks(p) {
			if c.typ == chunkData {
				ssns = append(ssns, binary.BigEndian.Uint16(c.value[6:]))
			}
		}
	}
	respond := func(v []byte) {
		t.Helper()
		if err := inject(a, n.now, chunkReconfig, 0, appendParam(nil, paramReconfigResponse, v)); err != nil {
			t.Fatal(err)
		}
	}
	result := func(seq, result uint32) []byte {
		return binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(nil, seq), result)
	}
	resetting := func() bool {
		return errors.Is(a.Send(1, PPIDBinary, []byte{1}, SendOptions{}, n.now), ErrStreamResetting)
	}
	seq := a.reset.nextOut
	respond(result(seq, resultPerformed))
	sendAll(t, a, n.now, 1, 1)
	if err := a.ResetStreams([]uint16{1}, n.now); err != nil {
		t.Fatal(err)
	}
	respond(result(seq+1, resultPerformed))
	respond(result(seq, resultPerformed)[:4])
	if !resetting() || len(done) != 0 {
		t.Fatal("a response to no request in flight ended the reset")
	}
	respond(result(seq, resultDenied))
	if resetting() || len(done) != 0 {
		t.Fatalf("denied: still resetting %v, %d resets done", resetting(), len(done))
	}
	// A zero window holds a message on the stream, and the next request
	// with it: a response repeated for the one before is ignored.
	sack(t, a, n.now, first-1, uint32(a.Stats().Flight))
	sendAll(t, a, n.now, 1)
	if err := a.ResetStreams([]uint16{1}, n.now); err != nil {
		t.Fatal(err)
	}
	respond(result(seq, resultPerformed))
	if !resetting() || len(done) != 0 {
		t.Fatal("a response repeated for the last request ended the reset waiting to go")
	}
	sack(t, a, n.now, first+2, 1<<20)
	respond(result(seq+1, resultPerformed))
	if resetting() || len(done) != 1 {
		t.Fatalf("performed: still resetting %v, %d resets done", resetting(), len(done))
	}
	if want := []uint16{0, 1, 2, 3, 0}; !slices.Equal(ssns, want) {
		t.Fatalf("sent SSNs %v, want %v", ssns, want)
	}
}

// TestResetWaitsForQueuedMessages covers a reset of a stream with a
// message queued on it: the request goes once the message is cut into
// chunks and covers them, while one queued on another stream does not
// hold it up.
func TestResetWaitsForQueuedMessages(t *testing.T) {
	for _, stream := range []uint16{1, 2} {
		n := newNetwork(t, 75, Config{})
		n.open(t, false)
		a := n.peers[0].assoc
		first := a.nextTSN
		var lasts []uint32
		a.send = func(p []byte) {
			for _, c := range packetChunks(p) {
				params, _ := parseParams(c.value)
				if c.typ == chunkReconfig && len(params) > 0 {
					lasts = append(lasts, binary.BigEndian.Uint32(params[0].value[8:]))
				}
			}
		}
		sendAll(t, a, n.now, 1)
		sack(t, a, n.now, first-1, uint32(a.Stats().Flight))
		if err := a.Send(stream, PPIDBinary, []byte{1}, SendOptions{}, n.now); err != nil {
			t.Fatal(err)
		}
		if err := a.ResetStreams([]uint16{1}, n.now); err != nil {
			t.Fatal(err)
		}
		want := []uint32{first}
		if stream == 1 {
			if len(lasts) != 0 {
				t.Fatal("the request went ahead of a message queued on its stream")
			}
			sack(t, a, n.now, first, 1<<20)
			want = []uint32{first + 1}
		}
		if !slices.Equal(lasts, want) {
			t.Fatalf("message on stream %d: requests with last TSNs %v, want %v", stream, lasts, want)
		}
	}
}

// TestResetTimer covers a reset request lost: it goes again after the
// retransmission timeout.
func TestResetTimer(t *testing.T) {
	n := newNetwork(t, 76, Config{})
	n.open(t, false)
	a := n.peers[0].assoc
	sent := recordPackets(a)
	if err := a.ResetStreams([]uint16{1}, n.now); err != nil {
		t.Fatal(err)
	}
	if err := a.HandleTimeout(a.Deadline()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(chunksSent(*sent), []byte{chunkReconfig, chunkReconfig}) || !a.Deadline().Equal(n.now.Add(2*a.Stats().RTO)) {
		t.Fatalf("sent %v, next at %v", chunksSent(*sent), a.Deadline().Sub(n.now))
	}
}
