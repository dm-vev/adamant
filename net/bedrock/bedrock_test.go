package bedrock

import (
	"bytes"
	"compress/flate"
	"errors"
	"io"
	"testing"
)

// pipe is a MessageConn whose messages go to its peer.
type pipe struct {
	in   chan []byte
	peer *pipe
	sent [][]byte
}

func pipes() (*pipe, *pipe) {
	a, b := &pipe{in: make(chan []byte, 64)}, &pipe{in: make(chan []byte, 64)}
	a.peer, b.peer = b, a
	return a, b
}

func (p *pipe) ReadMessage() ([]byte, error) {
	msg, ok := <-p.in
	if !ok {
		return nil, io.EOF
	}
	return msg, nil
}

func (p *pipe) Send(msg []byte) error {
	p.sent = append(p.sent, bytes.Clone(msg))
	p.peer.in <- bytes.Clone(msg)
	return nil
}

func (p *pipe) Close() error { close(p.peer.in); return nil }

func packet(id uint32, fields []byte) []byte {
	return append(AppendHeader(nil, Header{ID: id}), fields...)
}

func TestHeader(t *testing.T) {
	for _, h := range []Header{{ID: 1}, {ID: 0x3ff}, {ID: 193, SenderSub: 1, TargetSub: 3}} {
		b := AppendHeader(nil, h)
		got, rest, err := ReadHeader(append(b, 'x'))
		if err != nil || got != h || string(rest) != "x" {
			t.Fatalf("header %+v read back as %+v, %q, %v", h, got, rest, err)
		}
	}
	// RequestNetworkSettings: ID 193 is two bytes.
	if b := AppendHeader(nil, Header{ID: 193}); !bytes.Equal(b, []byte{0xc1, 0x01}) {
		t.Fatalf("header of 193 is % x", b)
	}
}

func TestPacketsLimits(t *testing.T) {
	body := AppendPacket(AppendPacket(nil, []byte{1, 2}), []byte{3})
	var got [][]byte
	if err := Packets(body, 10, func(p []byte) error { got = append(got, p); return nil }); err != nil || len(got) != 2 || got[1][0] != 3 {
		t.Fatalf("packets %v, %v", got, err)
	}
	for name, tc := range map[string]struct {
		body []byte
		max  int
		want error
	}{
		"too many":    {body, 1, errTooMany},
		"past end":    {[]byte{5, 1, 2}, 10, errPacketLength},
		"empty":       {[]byte{0}, 10, errEmptyPacket},
		"bad varint":  {[]byte{0x80}, 10, errPacketLength},
		"huge length": {[]byte{0xff, 0xff, 0xff, 0xff, 0x0f, 1}, 10, errPacketLength},
	} {
		if err := Packets(tc.body, tc.max, func([]byte) error { return nil }); !errors.Is(err, tc.want) {
			t.Fatalf("%s: %v, want %v", name, err, tc.want)
		}
	}
}

func TestConnRoundTrip(t *testing.T) {
	a, b := pipes()
	ca, cb := NewConn(a, Config{}), NewConn(b, Config{})
	// Before NetworkSettings a batch has no algorithm byte.
	_ = ca.WritePacket(packet(193, []byte{0, 0, 8, 0x91}))
	_ = ca.Flush()
	if got := a.sent[0]; !bytes.Equal(got, []byte{4, 0xc1, 0x01, 0, 0, 8, 0x91}) && got[0] != 6 {
		t.Fatalf("first batch % x", got)
	}
	p, err := cb.ReadPacket()
	if err != nil || !bytes.Equal(p, packet(193, []byte{0, 0, 8, 0x91})) {
		t.Fatalf("read % x, %v", p, err)
	}

	ca.EnableCompression(Flate{})
	cb.EnableCompression(Flate{})
	small := packet(9, []byte("hello"))
	large := packet(10, bytes.Repeat([]byte("chunk data "), 100))
	_ = ca.WritePacket(small)
	_ = ca.Flush()
	_ = ca.WritePacket(small)
	_ = ca.WritePacket(large)
	_ = ca.Flush()
	if a.sent[1][0] != 0xff || a.sent[2][0] != 0 || len(a.sent[2]) > len(large)/4 {
		t.Fatalf("algorithm bytes %#x and %#x, compressed %d of %d bytes", a.sent[1][0], a.sent[2][0], len(a.sent[2]), len(large))
	}
	for _, want := range [][]byte{small, small, large} {
		if p, err := cb.ReadPacket(); err != nil || !bytes.Equal(p, want) {
			t.Fatalf("read %d bytes, %v; want %d", len(p), err, len(want))
		}
	}
}

// TestIncompressibleBatchesGoPlain sends a batch of movement as it is,
// however large, and compresses a batch that mixes in anything else.
func TestIncompressibleBatchesGoPlain(t *testing.T) {
	a, b := pipes()
	movement := func(id uint32) bool { return id == 18 }
	ca, cb := NewConn(a, Config{Incompressible: movement}), NewConn(b, Config{})
	ca.EnableCompression(Flate{})
	cb.EnableCompression(Flate{})
	move := packet(18, bytes.Repeat([]byte{7}, 100))
	for range 10 {
		_ = ca.WritePacket(move)
	}
	_ = ca.Flush()
	_ = ca.WritePacket(move)
	_ = ca.WritePacket(packet(9, bytes.Repeat([]byte("text "), 100)))
	_ = ca.Flush()
	if a.sent[0][0] != 0xff || a.sent[1][0] != 0 {
		t.Fatalf("algorithm bytes %#x and %#x", a.sent[0][0], a.sent[1][0])
	}
	for range 12 {
		if _, err := cb.ReadPacket(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMaxBodySplitsBatches(t *testing.T) {
	a, b := pipes()
	ca, cb := NewConn(a, Config{MaxBody: 1000}), NewConn(b, Config{})
	for i := range 10 {
		_ = ca.WritePacket(packet(uint32(i+1), make([]byte, 300)))
	}
	_ = ca.Flush()
	if len(a.sent) != 4 {
		t.Fatalf("%d messages for 10 packets of 300 bytes", len(a.sent))
	}
	for i := range 10 {
		p, err := cb.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if h, _, _ := ReadHeader(p); h.ID != uint32(i+1) {
			t.Fatalf("packet %d has ID %d", i, h.ID)
		}
	}
}

// TestBatchLimits refuses a batch that inflates past MaxBatch and one that
// names an unknown algorithm.
func TestBatchLimits(t *testing.T) {
	var bomb bytes.Buffer
	w, _ := flate.NewWriter(&bomb, flate.BestCompression)
	_, _ = w.Write(make([]byte, 4<<20))
	_ = w.Close()
	for name, tc := range map[string]struct {
		msg  []byte
		want error
	}{
		"bomb":      {append([]byte{0}, bomb.Bytes()...), ErrTooLarge},
		"plain":     {append([]byte{0xff}, make([]byte, 2<<20)...), ErrTooLarge},
		"algorithm": {[]byte{7, 1, 2}, errAlgorithm},
	} {
		a, b := pipes()
		cb := NewConn(b, Config{MaxBatch: 1 << 20})
		cb.EnableCompression(Flate{})
		_ = a.Send(tc.msg)
		if _, err := cb.ReadPacket(); !errors.Is(err, tc.want) {
			t.Fatalf("%s: %v, want %v", name, err, tc.want)
		}
	}
}

func TestFlateLevels(t *testing.T) {
	src := bytes.Repeat([]byte("Minecraft batch "), 1000)
	for _, level := range []int{0, 1, 6, 9, 42} {
		c, err := Flate{Level: level}.Compress([]byte("prefix"), src)
		if err != nil || !bytes.HasPrefix(c, []byte("prefix")) {
			t.Fatalf("level %d: %v", level, err)
		}
		d, err := Flate{}.Decompress(nil, c[len("prefix"):], len(src))
		if err != nil || !bytes.Equal(d, src) {
			t.Fatalf("level %d: inflated %d bytes, %v", level, len(d), err)
		}
	}
	c, _ := Flate{}.Compress(nil, src)
	if _, err := (Flate{}).Decompress(nil, c, len(src)-1); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("decompress past the limit: %v", err)
	}
}

func FuzzReadBatch(f *testing.F) {
	f.Add([]byte{0xff, 2, 1, 2})
	c, _ := Flate{}.Compress([]byte{0}, AppendPacket(nil, []byte{9, 1}))
	f.Add(c)
	f.Fuzz(func(t *testing.T, msg []byte) {
		a, b := pipes()
		cb := NewConn(b, Config{MaxBatch: 1 << 16, MaxPackets: 64})
		cb.EnableCompression(Flate{})
		_ = a.Send(msg)
		_ = a.Close()
		for range 70 {
			if _, err := cb.ReadPacket(); err != nil {
				return
			}
		}
		t.Fatal("read more packets than the limit allows")
	})
}

// TestSendBatch sends a batch encoded once, after the packets gathered
// before it, on two connections.
func TestSendBatch(t *testing.T) {
	recipes := packet(52, bytes.Repeat([]byte("recipe "), 2000))
	msg, err := EncodeBatch(Flate{Level: 9, FixedBelow: -1}, recipes)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		a, b := pipes()
		ca, cb := NewConn(a, Config{}), NewConn(b, Config{})
		ca.EnableCompression(Flate{})
		cb.EnableCompression(Flate{})
		if err := ca.SendBatch([]byte{1, 0}); err == nil {
			t.Fatal("sent a batch of another compression")
		}
		before, after := packet(9, []byte("before")), packet(9, []byte("after"))
		_ = ca.WritePacket(before)
		if err := ca.SendBatch(msg); err != nil {
			t.Fatal(err)
		}
		_ = ca.WritePacket(after)
		_ = ca.Flush()
		for _, want := range [][]byte{before, recipes, after} {
			if p, err := cb.ReadPacket(); err != nil || !bytes.Equal(p, want) {
				t.Fatalf("read %d bytes, %v; want %d", len(p), err, len(want))
			}
		}
	}
}

// TestRateLimits fails the read of a peer that runs past the burst of
// packets or of bytes it may send.
func TestRateLimits(t *testing.T) {
	for _, c := range []struct {
		name string
		cfg  Config
		pk   []byte
	}{
		{"packets", Config{MaxPackets: 10, PacketRate: 10, PacketBurst: 20}, packet(9, []byte("hi"))},
		{"bytes", Config{MaxBatch: 1000, ByteRate: 1000, ByteBurst: 2000}, packet(9, bytes.Repeat([]byte{1}, 95))},
	} {
		a, b := pipes()
		ca, cb := NewConn(a, Config{}), NewConn(b, c.cfg)
		send := func() {
			for range 10 {
				_ = ca.WritePacket(c.pk)
			}
			_ = ca.Flush()
		}
		// Two batches fit the burst; the third, at once, does not.
		for i := range 3 {
			send()
			var err error
			for range 10 {
				if _, err = cb.ReadPacket(); err != nil {
					break
				}
			}
			if i < 2 && err != nil || i == 2 && !errors.Is(err, ErrRateLimited) {
				t.Fatalf("%s: batch %d read with %v", c.name, i, err)
			}
		}
	}
}

// TestHandleBatch reads batches handed over by a receiver of the
// transport: the packets ReadPacket split off first, then each packet of
// a batch in order, under the rates of ReadPacket.
func TestHandleBatch(t *testing.T) {
	a, b := pipes()
	ca, cb := NewConn(a, Config{}), NewConn(b, Config{MaxPackets: 3, PacketRate: 10, PacketBurst: 6})
	ca.EnableCompression(Flate{})
	cb.EnableCompression(Flate{})
	for i := range 3 {
		_ = ca.WritePacket(packet(9, []byte{byte(i)}))
	}
	_ = ca.Flush()
	if p, err := cb.ReadPacket(); err != nil || p[len(p)-1] != 0 {
		t.Fatalf("read % x, %v", p, err)
	}
	var got []byte
	for _, p := range cb.TakePending() {
		got = append(got, p[len(p)-1])
	}
	for i := 3; i < 6; i++ {
		_ = ca.WritePacket(packet(9, []byte{byte(i)}))
	}
	_ = ca.Flush()
	if err := cb.HandleBatch(<-b.in, func(p []byte) error {
		got = append(got, p[len(p)-1])
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte{1, 2, 3, 4, 5}) {
		t.Fatalf("packets % x", got)
	}
	// The rates count what ReadPacket and HandleBatch read alike.
	_ = ca.WritePacket(packet(9, []byte{6}))
	_ = ca.Flush()
	if err := cb.HandleBatch(<-b.in, func([]byte) error { return nil }); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("batch over the rate read with %v", err)
	}
	stop := errors.New("stop")
	_ = ca.WritePacket(packet(9, nil))
	_ = ca.Flush()
	fresh := NewConn(b, Config{})
	fresh.EnableCompression(Flate{})
	if err := fresh.HandleBatch(<-b.in, func([]byte) error { return stop }); err != stop {
		t.Fatalf("an error of f came back as %v", err)
	}
}

func TestFlateEdgeCasesAndParallel(t *testing.T) {
	// Empty input with limit 0 succeeds; 1-byte input with limit 0 fails.
	emptyC, err := (Flate{}).Compress(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := (Flate{}).Decompress([]byte("P:"), emptyC, 0); err != nil || string(got) != "P:" {
		t.Fatalf("empty decompress limit 0: got=%q err=%v", got, err)
	}
	oneC, err := (Flate{}).Compress(nil, []byte{'x'})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (Flate{}).Decompress(nil, oneC, 0); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("1-byte decompress limit 0: err=%v, want %v", err, ErrTooLarge)
	}

	// Boundary sizes across fixedDeflate and stdlib flate thresholds.
	for _, n := range []int{0, 1, 2, 3, 4, 5, 6, 7, 15, 16, 127, 128, 129, 16383, 16384, 16385, 32768, 40000} {
		src := make([]byte, n)
		for i := range src {
			src[i] = byte((i*31 + i/7) & 0xff)
		}
		for _, comp := range []Flate{{}, {Level: 1, FixedBelow: -1}} {
			c, err := comp.Compress([]byte{0x42}, src)
			if err != nil || c[0] != 0x42 {
				t.Fatalf("n=%d comp=%+v compress err=%v", n, comp, err)
			}
			got, err := comp.Decompress([]byte("HDR"), c[1:], n)
			if err != nil || !bytes.HasPrefix(got, []byte("HDR")) || !bytes.Equal(got[3:], src) {
				t.Fatalf("n=%d comp=%+v round-trip failed: len=%d err=%v", n, comp, len(got), err)
			}
			if n > 0 {
				if _, err := comp.Decompress(nil, c[1:], n-1); !errors.Is(err, ErrTooLarge) {
					t.Fatalf("n=%d comp=%+v limit n-1: err=%v, want ErrTooLarge", n, comp, err)
				}
			}
		}
	}
}

func TestFramingVarintBoundariesAndPendingReuse(t *testing.T) {
	// Test 1-byte, 2-byte, and 3-byte packet lengths and header IDs.
	lengths := []int{1, 126, 127, 128, 16382, 16383, 16384, 20000}
	var pks [][]byte
	for i, n := range lengths {
		id := uint32([]int{0, 1, 127, 128, 193, 511, 1023, 42}[i])
		h := Header{ID: id, SenderSub: uint8(i & 3), TargetSub: uint8((i >> 1) & 3)}
		p := AppendHeader(nil, h)
		if len(p) < n {
			p = append(p, bytes.Repeat([]byte{byte(i + 1)}, n-len(p))...)
		}
		gotH, rest, err := ReadHeader(p)
		if err != nil || gotH != h || len(rest) != len(p)-len(AppendHeader(nil, h)) {
			t.Fatalf("header %+v mismatch: got=%+v err=%v", h, gotH, err)
		}
		pks = append(pks, p)
	}
	msg, err := EncodeBatch(Flate{}, pks...)
	if err != nil {
		t.Fatal(err)
	}
	a, b := pipes()
	cb := NewConn(b, Config{MaxBatch: 1 << 20})
	cb.EnableCompression(Flate{})
	_ = a.Send(msg)
	// Read first 2 packets, take next pending, then send another batch to test slice reuse.
	for i := range 2 {
		p, err := cb.ReadPacket()
		if err != nil || !bytes.Equal(p, pks[i]) {
			t.Fatalf("packet %d mismatch: err=%v", i, err)
		}
	}
	rem := cb.TakePending()
	if len(rem) != len(pks)-2 {
		t.Fatalf("TakePending returned %d packets, want %d", len(rem), len(pks)-2)
	}
	for i, p := range rem {
		if !bytes.Equal(p, pks[i+2]) {
			t.Fatalf("pending packet %d mismatch", i+2)
		}
	}
	// Next batch after TakePending + draining >64 packets.
	var many [][]byte
	for i := range 70 {
		many = append(many, packet(9, []byte{byte(i)}))
	}
	msg2, err := EncodeBatch(Flate{}, many...)
	if err != nil {
		t.Fatal(err)
	}
	_ = a.Send(msg2)
	for i := range many {
		p, err := cb.ReadPacket()
		if err != nil || !bytes.Equal(p, many[i]) {
			t.Fatalf("many[%d] mismatch: err=%v", i, err)
		}
	}
}
