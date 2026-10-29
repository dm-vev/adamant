package bedrock

import (
	"bytes"
	"compress/flate"
	"errors"
	"fmt"
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
	if got := a.sent[0]; !bytes.Equal(got, []byte{4, 0xc1, 0x01, 0, 0, 8, 0x91}[:]) && got[0] != 6 {
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
		t.Fatal(fmt.Sprint("read more packets than the limit allows"))
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
