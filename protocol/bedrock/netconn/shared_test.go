package netconn

import (
	"bytes"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/internal/faultcheck"
	"github.com/df-mc/dragonfly/protocol/bedrock/internal/versions"
	"github.com/dm-vev/adamant/net/bedrock"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// TestSharedIsEncodedOncePerClass writes a Shared of a packet of the same
// layout in 2193 and the model, and one that 2225 changed: the first goes
// in the model's bytes to every protocol, encoded once; the second in the
// model's to the model and in 2193's, encoded once more, to 2193.
func TestSharedIsEncodedOncePerClass(t *testing.T) {
	text := &Shared{Packet: &mc.TextPacket{TextType: mc.TextTypeRaw, Message: "hi"}}
	model := text.encoded(versions.Model())
	if len(model) != 1 || &text.encoded(nil)[0].raw[0] != &model[0].raw[0] || &text.encoded(oldProtocol())[0].raw[0] != &model[0].raw[0] || text.wires != nil {
		t.Fatal("a Text is not encoded once for every protocol")
	}
	if !bytes.Equal(model[0].framed, bedrock.AppendPacket(nil, model[0].raw)) || model[0].id != mc.IDText {
		t.Fatal("the Shared framed bytes or ID do not match raw")
	}
	var w wire.Writer
	text.Encode(&w)
	if _, fields, _ := bedrock.ReadHeader(model[0].raw); !bytes.Equal(w.B, fields) {
		t.Fatal("the Shared encodes otherwise than it writes")
	}

	animate := &Shared{Packet: &mc.AnimatePacket{ActionType: mc.AnimateActionSwingArm, EntityRuntimeID: 7, Hand: 1}}
	old := animate.encoded(oldProtocol())
	if len(old) != 1 || &animate.encoded(oldProtocol())[0].raw[0] != &old[0].raw[0] || len(animate.wires) != 1 {
		t.Fatal("an Animate is not encoded once for 2193")
	}
	if !bytes.Equal(old[0].framed, bedrock.AppendPacket(nil, old[0].raw)) || old[0].id != mc.IDAnimate {
		t.Fatal("the converted Shared framed bytes or ID do not match raw")
	}
	h, fields, err := bedrock.ReadHeader(old[0].raw)
	var down v2193.AnimatePacket
	r := wire.NewReader(fields)
	down.Decode(r)
	if err != nil || h.ID != mc.IDAnimate || r.Err() != nil || r.Len() != 0 || down.EntityRuntimeID != 7 {
		t.Fatalf("the Animate of 2193 is % x", old[0].raw)
	}
	if now := animate.encoded(nil); bytes.Equal(now[0].raw, old[0].raw) {
		t.Fatal("the model writes the Animate as 2193 does")
	}
}

// TestConnWritesShared writes a Shared through a connection of 2193.
func TestConnWritesShared(t *testing.T) {
	l := &Listener{batches: bedrock.Config{Cache: bedrock.NewBatchCache()}}
	c := newConn(l, discard{}, nil)
	c.comp = bedrock.None{}
	c.b.EnableCompression(c.comp)
	c.proto = oldProtocol()
	pk := &Shared{Packet: &mc.AnimatePacket{ActionType: mc.AnimateActionSwingArm, EntityRuntimeID: 7}}
	if err := c.WritePacket(pk); err != nil {
		t.Fatal(err)
	}
	if pk.wires[oldProtocol()] == nil {
		t.Fatal("the connection did not write the Animate of 2193")
	}
	if n := testing.AllocsPerRun(100, func() { _ = c.WritePacket(pk) }); n != 0 {
		t.Errorf("writing a Shared encoded allocates %v times", n)
	}
}

type recordTransport struct {
	sent [][]byte
	err  error
}

func (t *recordTransport) ReadMessage() ([]byte, error)    { return nil, io.EOF }
func (t *recordTransport) Close() error                    { return nil }
func (t *recordTransport) SetReadDeadline(time.Time) error { return nil }
func (t *recordTransport) SetReceiver(func([]byte))        {}
func (t *recordTransport) OnClose(func(error))             {}
func (t *recordTransport) RemoteAddr() net.Addr            { return &net.UDPAddr{} }
func (t *recordTransport) Send(msg []byte) error {
	if t.err != nil {
		return t.err
	}
	t.sent = append(t.sent, bytes.Clone(msg))
	return nil
}

type panicTestPacket struct{ proto.Packet }

func (panicTestPacket) ID() uint32          { return mc.IDText }
func (panicTestPacket) Encode(*wire.Writer) { panic(faultcheck.Expected{Why: "encode boom"}) }

func decodeSentPackets(t *testing.T, comp bedrock.Compression, msgs [][]byte) []proto.Packet {
	t.Helper()
	var out []proto.Packet
	for _, msg := range msgs {
		var body []byte
		switch msg[0] {
		case byte(bedrock.AlgorithmNone & 0xff):
			body = msg[1:]
		case byte(comp.Algorithm()):
			var err error
			body, err = comp.Decompress(nil, msg[1:], 1<<20)
			if err != nil {
				t.Fatalf("decompress: %v", err)
			}
		default:
			t.Fatalf("unexpected algorithm byte %#x", msg[0])
		}
		if err := bedrock.Packets(body, 1024, func(raw []byte) error {
			pk, err := DecodePacket(raw)
			if err != nil {
				return err
			}
			out = append(out, pk)
			return nil
		}); err != nil {
			t.Fatalf("packets: %v", err)
		}
	}
	return out
}

func TestConnWriteBatchSharedPersonalStaticAndPanic(t *testing.T) {
	l := &Listener{batches: bedrock.Config{Cache: bedrock.NewBatchCache()}}
	tr := &recordTransport{}
	c := newConn(l, tr, nil)
	c.comp = bedrock.Flate{Level: 1}
	c.b.EnableCompression(c.comp)
	c.proto = versions.Model()

	sharedPks := make([]*Shared, 12)
	for i := range sharedPks {
		sharedPks[i] = &Shared{Packet: &mc.TextPacket{TextType: mc.TextTypeRaw, Message: strings.Repeat("shared-msg-", 4) + string(rune('a'+i))}}
	}
	personal1 := &mc.TextPacket{TextType: mc.TextTypeRaw, Message: strings.Repeat("personal-one-", 25)}
	personal2 := &mc.TextPacket{TextType: mc.TextTypeRaw, Message: strings.Repeat("personal-two-", 25)}
	st := &staticText{&mc.TextPacket{TextType: mc.TextTypeRaw, Message: strings.Repeat("static-msg-", 25)}}
	bad := panicTestPacket{&mc.TextPacket{}}

	batch := []proto.Packet{personal1, bad}
	for _, sh := range sharedPks[:6] {
		batch = append(batch, sh)
	}
	batch = append(batch, personal2, st)
	for _, sh := range sharedPks[6:] {
		batch = append(batch, sh)
	}

	var panicked []proto.Packet
	if err := c.WriteBatch(batch, func(pk proto.Packet, _ any) {
		panicked = append(panicked, pk)
	}); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	if len(panicked) != 1 || panicked[0] != bad {
		t.Fatalf("panicked = %v, want [%v]", panicked, bad)
	}
	got := decodeSentPackets(t, c.comp, tr.sent)
	wantCount := 1 + 6 + 1 + 1 + 6
	if len(got) != wantCount {
		t.Fatalf("decoded %d packets, want %d", len(got), wantCount)
	}
	if got[0].(*mc.TextPacket).Message != personal1.Message {
		t.Fatalf("got[0] = %q, want personal1", got[0].(*mc.TextPacket).Message)
	}
	if got[7].(*mc.TextPacket).Message != personal2.Message {
		t.Fatalf("got[7] = %q, want personal2", got[7].(*mc.TextPacket).Message)
	}
	if got[8].(*mc.TextPacket).Message != st.Packet.(*mc.TextPacket).Message {
		t.Fatalf("got[8] = %q, want static", got[8].(*mc.TextPacket).Message)
	}

	// Send error stops without flushing remainder or losing error.
	wantErr := errors.New("send failed")
	tr.err = wantErr
	tr.sent = nil
	if err := c.WriteBatch([]proto.Packet{personal1}, nil); !errors.Is(err, wantErr) {
		t.Fatalf("WriteBatch err = %v, want %v", err, wantErr)
	}

	// Steady-state WriteBatch of pre-encoded Shared packets allocates 0 times.
	cAlloc := newConn(&Listener{batches: bedrock.Config{Cache: bedrock.NewBatchCache()}}, discard{}, nil)
	cAlloc.comp = bedrock.None{}
	cAlloc.b.EnableCompression(cAlloc.comp)
	cAlloc.proto = oldProtocol()
	sharedBatch := []proto.Packet{
		&Shared{Packet: &mc.AnimatePacket{ActionType: mc.AnimateActionSwingArm, EntityRuntimeID: 7}},
		&Shared{Packet: &mc.TextPacket{TextType: mc.TextTypeRaw, Message: "hi"}},
	}
	if err := cAlloc.WriteBatch(sharedBatch, nil); err != nil {
		t.Fatal(err)
	}
	if n := testing.AllocsPerRun(100, func() { _ = cAlloc.WriteBatch(sharedBatch, nil) }); n != 0 {
		t.Errorf("steady-state WriteBatch of Shared allocated %v times", n)
	}
}

func BenchmarkNetconnWriteBatchCrowd100(b *testing.B) {
	const (
		numPlayers = 100
		numTicks   = 256
	)
	batches := make([][][]proto.Packet, numTicks)
	for t := range numTicks {
		shared := make([]proto.Packet, 48)
		for k := range shared {
			shared[k] = &Shared{Packet: &mc.MoveActorDeltaPacket{
				EntityRuntimeID: uint64(100 + k),
				PositionX:       proto.Some(float32(t + k)),
				PositionY:       proto.Some(float32(64)),
				PositionZ:       proto.Some(float32(k)),
			}}
		}
		tick := make([][]proto.Packet, numPlayers)
		for p := range numPlayers {
			batch := make([]proto.Packet, 0, len(shared)+2)
			batch = append(batch, shared...)
			batch = append(batch,
				&mc.SetTimePacket{Time: int32(t*1000 + p)},
				&mc.TextPacket{TextType: mc.TextTypeRaw, Message: strings.Repeat("p", 260) + string(rune('a'+(p%26)))},
			)
			tick[p] = batch
		}
		batches[t] = tick
	}

	run := func(name string, comp bedrock.Compression, useBatch bool) {
		b.Run(name, func(b *testing.B) {
			l := &Listener{batches: bedrock.Config{Cache: bedrock.NewBatchCache()}}
			conns := make([]*Conn, numPlayers)
			for i := range numPlayers {
				c := newConn(l, discard{}, nil)
				c.comp = comp
				c.b.EnableCompression(comp)
				c.proto = versions.Model()
				conns[i] = c
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; b.Loop(); i++ {
				tick := batches[i%numTicks]
				for p := range numPlayers {
					if useBatch {
						if err := conns[p].WriteBatch(tick[p], nil); err != nil {
							b.Fatal(err)
						}
					} else {
						for _, pk := range tick[p] {
							if err := conns[p].WritePacket(pk); err != nil {
								b.Fatal(err)
							}
						}
						if err := conns[p].Flush(); err != nil {
							b.Fatal(err)
						}
					}
				}
			}
		})
	}
	run("WholeFlate", bedrock.Flate{Level: 1}, false)
	run("SegmentedFlate", bedrock.Flate{Level: 1}, true)
	run("Snappy", bedrock.Snappy{}, true)
}
