package sctp

import (
	"bytes"
	"errors"
	"slices"
	"testing"
	"time"
)

// packetOf returns a packet from port 5000 to port 5000 with the tag and
// chunks given, each chunk a type and a value.
func packetOf(tag uint32, chunks ...[]byte) []byte {
	var p packetBuilder
	p.start(header{srcPort: 5000, dstPort: 5000, tag: tag})
	for _, c := range chunks {
		p.addChunk(c[0], 0, c[1:])
	}
	return bytes.Clone(p.finish())
}

// recordPackets has an association record the packets it sends from now
// on.
func recordPackets(a *Association) *[][]byte {
	var sent [][]byte
	a.send = func(p []byte) { sent = append(sent, bytes.Clone(p)) }
	return &sent
}

// chunksSent returns the types of the chunks of the packets given.
func chunksSent(packets [][]byte) []byte {
	var types []byte
	for _, p := range packets {
		for _, c := range packetChunks(p) {
			types = append(types, c.typ)
		}
	}
	return types
}

// TestInitChecked covers the INITs an association answers: one alone in
// its packet with a tag of zero (RFC 9260 section 8.5.1), of at least the
// fixed 16 bytes, with a tag and streams each way (section 3.3.2) and
// parameters that parse.
func TestInitChecked(t *testing.T) {
	init := initValue(7, 100)
	without := func(off int) []byte {
		v := bytes.Clone(init)
		v[off], v[off+1] = 0, 0
		return v
	}
	heartbeat := []byte{chunkHeartbeat, 1, 2, 3, 4}
	cases := []struct {
		name     string
		packet   []byte
		answered bool
	}{
		{"fixed part only", packetOf(0, append([]byte{chunkInit}, init...)), true},
		{"with parameters", packetOf(0, append([]byte{chunkInit}, initValue(7, 100, appendParam(nil, paramForwardTSN, nil))...)), true},
		{"short", packetOf(0, append([]byte{chunkInit}, init[:15]...)), false},
		{"tag of zero", packetOf(0, append([]byte{chunkInit}, initValue(0, 100)...)), false},
		{"no outbound stream", packetOf(0, append([]byte{chunkInit}, without(8)...)), false},
		{"no inbound stream", packetOf(0, append([]byte{chunkInit}, without(10)...)), false},
		{"malformed parameter", packetOf(0, append([]byte{chunkInit}, append(bytes.Clone(init), 0, 1, 0, 2)...)), false},
		{"verification tag", packetOf(5, append([]byte{chunkInit}, init...)), false},
		{"bundled", packetOf(0, append([]byte{chunkInit}, init...), heartbeat), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := New(Config{}, nil)
			sent := recordPackets(b)
			if err := b.HandlePacket(c.packet, time.Unix(1, 0)); err != nil {
				t.Fatal(err)
			}
			if answered := slices.Equal(chunksSent(*sent), []byte{chunkInitAck}); answered != c.answered {
				t.Fatalf("answered with %v, want an INIT ACK: %v", chunksSent(*sent), c.answered)
			}
		})
	}
}

// TestRepeatedInitGetsSameAnswer covers an INIT sent again because the
// INIT ACK was lost: the same INIT ACK goes again, while an INIT with
// another tag gets a new cookie.
func TestRepeatedInitGetsSameAnswer(t *testing.T) {
	b := New(Config{}, nil)
	sent := recordPackets(b)
	now := time.Unix(1, 0)
	for _, tag := range []uint32{7, 7, 8} {
		if err := b.HandlePacket(packetOf(0, append([]byte{chunkInit}, initValue(tag, 100)...)), now); err != nil {
			t.Fatal(err)
		}
	}
	if len(*sent) != 3 || !bytes.Equal((*sent)[0], (*sent)[1]) {
		t.Fatalf("%d INIT ACKs, the second the same: %v", len(*sent), len(*sent) > 1 && bytes.Equal((*sent)[0], (*sent)[1]))
	}
	cookie := func(p []byte) []byte {
		ack, _ := parseInit(packetChunks(p)[0].value)
		return ack.cookie
	}
	if bytes.Equal(cookie((*sent)[0]), cookie((*sent)[2])) {
		t.Fatal("an INIT with another tag got the same cookie")
	}
}

// TestCookieEchoChecked covers the COOKIE ECHO that opens an association:
// it must echo the cookie of the INIT ACK, carry the tag of this side and
// come first in its packet (RFC 9260 sections 5.1 and 8.5.1).
func TestCookieEchoChecked(t *testing.T) {
	cases := []struct {
		name   string
		packet func(tag uint32, cookie []byte) []byte
		opens  bool
	}{
		{"echoed", func(tag uint32, cookie []byte) []byte {
			return packetOf(tag, append([]byte{chunkCookieEcho}, cookie...))
		}, true},
		{"other cookie", func(tag uint32, cookie []byte) []byte {
			return packetOf(tag, append([]byte{chunkCookieEcho}, make([]byte, len(cookie))...))
		}, false},
		{"other tag", func(tag uint32, cookie []byte) []byte {
			return packetOf(tag+1, append([]byte{chunkCookieEcho}, cookie...))
		}, false},
		{"not first", func(tag uint32, cookie []byte) []byte {
			return packetOf(tag, []byte{chunkHeartbeat, 1, 2, 3, 4}, append([]byte{chunkCookieEcho}, cookie...))
		}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := New(Config{}, nil)
			sent := recordPackets(b)
			now := time.Unix(1, 0)
			if err := b.HandlePacket(packetOf(0, append([]byte{chunkInit}, initValue(7, 100)...)), now); err != nil {
				t.Fatal(err)
			}
			ack, _ := parseInit(packetChunks((*sent)[0])[0].value)
			*sent = nil
			if err := b.HandlePacket(c.packet(ack.tag, ack.cookie), now); err != nil {
				t.Fatal(err)
			}
			acked := slices.Contains(chunksSent(*sent), chunkCookieAck)
			if b.Established() != c.opens || acked != c.opens {
				t.Fatalf("established %v, COOKIE ACK sent %v; want %v", b.Established(), acked, c.opens)
			}
		})
	}
}

// TestPortsChecked covers packets to or from another port than those of
// the association: they are ignored.
func TestPortsChecked(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	a := connectTo(t, now)
	sent := recordPackets(a)
	for _, ports := range [][2]uint16{{5000, 5001}, {5001, 5000}} {
		var p packetBuilder
		p.start(header{srcPort: ports[0], dstPort: ports[1], tag: a.localTag})
		p.addChunk(chunkHeartbeat, 0, []byte{1, 2, 3, 4})
		if err := a.HandlePacket(p.finish(), now); err != nil {
			t.Fatal(err)
		}
	}
	if len(*sent) != 0 {
		t.Fatalf("answered %v to packets of other ports", chunksSent(*sent))
	}
}

// TestUnknownChunks covers chunk types this side does not know: one with
// the top bit set is skipped and the packet goes on, one with it clear
// stops the processing of the packet (RFC 9260 section 3.2).
func TestUnknownChunks(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	for _, typ := range []byte{0xc1, 0x81, 0x3f, 0x41} {
		a := connectTo(t, now)
		sent := recordPackets(a)
		if err := a.HandlePacket(packetOf(a.localTag, []byte{typ}, []byte{chunkHeartbeat, 1, 2, 3, 4}), now); err != nil {
			t.Fatal(err)
		}
		if answered := slices.Contains(chunksSent(*sent), chunkHeartbeatAck); answered != (typ&0x80 != 0) {
			t.Fatalf("chunk type %#x: the HEARTBEAT after it answered: %v", typ, answered)
		}
	}
}

// TestInitRetransmissionsBounded covers a peer that never answers the
// INIT: it goes eight times again, the interval doubling, then the
// association fails.
func TestInitRetransmissionsBounded(t *testing.T) {
	a := New(Config{}, nil)
	sent := recordPackets(a)
	now := time.Unix(1_700_000_000, 0)
	a.Connect(now)
	var err error
	var intervals []time.Duration
	for err == nil {
		intervals = append(intervals, a.Deadline().Sub(now))
		now = a.Deadline()
		err = a.HandleTimeout(now)
	}
	if !errors.Is(err, ErrTimeout) || !slices.Equal(chunksSent(*sent), slices.Repeat([]byte{chunkInit}, 1+maxInitRetrans)) {
		t.Fatalf("sent %v, then %v", chunksSent(*sent), err)
	}
	if intervals[0] != rtoInitial || intervals[1] != 2*rtoInitial {
		t.Fatalf("intervals %v, want doubling from %v", intervals, rtoInitial)
	}
}

// TestPeerParamsBounded covers an INIT ACK with many parameters: the
// association keeps the types of 32 of them for Stats.
func TestPeerParamsBounded(t *testing.T) {
	params := slices.Repeat([][]byte{appendParam(nil, 0x8001, nil)}, 2*maxPeerFeatures)
	a := connectTo(t, time.Unix(1_700_000_000, 0), params...)
	if got := len(a.Stats().Peer.Params); got != maxPeerFeatures {
		t.Fatalf("kept %d parameter types, want %d", got, maxPeerFeatures)
	}
}

// TestForwardTSNInSupportedExtensions covers a peer that offers FORWARD
// TSN in its Supported Extensions alone: a partially reliable message lost
// is given up and skipped rather than sent again.
func TestForwardTSNInSupportedExtensions(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	a := connectTo(t, now, appendParam(nil, paramSupportedExtension, []byte{chunkForwardTSN}))
	sent := recordPackets(a)
	if err := a.Send(1, PPIDBinary, []byte("lost"), SendOptions{Unordered: true, Partial: true}, now); err != nil {
		t.Fatal(err)
	}
	for a.Stats().Timeouts == 0 {
		if err := a.HandleTimeout(a.Deadline()); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(chunksSent(*sent), []byte{chunkData, chunkForwardTSN}) {
		t.Fatalf("sent %v, want the DATA, then a FORWARD TSN after the timeout", chunksSent(*sent))
	}
}
