package session

import (
	"io"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"

	"github.com/dm-vev/adamant/net/bedrock"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/net/bedrock/wire"
)

type crowdDiscardTransport struct {
	sent atomic.Uint64
}

func (t *crowdDiscardTransport) ReadMessage() ([]byte, error) { return nil, io.EOF }
func (t *crowdDiscardTransport) Close() error                 { return nil }
func (t *crowdDiscardTransport) Send(msg []byte) error {
	t.sent.Add(uint64(len(msg)))
	return nil
}

// crowdShared mirrors netconn.Shared's once-per-wire-class pre-framed fields
// so the 100-session benchmark isolates Session.writeBatch and compression cost
// without socket I/O.
type crowdShared struct {
	proto.Packet
	once   sync.Once
	raw    []byte
	framed []byte
	id     uint32
}

func (s *crowdShared) encoded() ([]byte, []byte, uint32) {
	s.once.Do(func() {
		s.id = s.Packet.ID()
		w := wire.Writer{B: bedrock.AppendHeader(nil, bedrock.Header{ID: s.id})}
		s.Packet.Encode(&w)
		s.raw = w.B
		s.framed = bedrock.AppendPacket(nil, w.B)
	})
	return s.raw, s.framed, s.id
}

// crowdWholeConn implements session.Conn without batchPacketWriter so
// Session.writeBatch uses the per-packet fallback + Flush (whole-batch compression).
type crowdWholeConn struct {
	benchmarkConn
	b *bedrock.Conn
	w wire.Writer
}

func (c *crowdWholeConn) WritePacket(pk proto.Packet) error {
	if sh, ok := pk.(*crowdShared); ok {
		raw, _, _ := sh.encoded()
		return c.b.WritePacket(raw)
	}
	c.w.B = bedrock.AppendHeader(c.w.B[:0], bedrock.Header{ID: pk.ID()})
	pk.Encode(&c.w)
	return c.b.WritePacket(c.w.B)
}

func (c *crowdWholeConn) Flush() error { return c.b.Flush() }

// crowdBatchConn implements batchPacketWriter (single-lock WriteBatch +
// pre-framed Shared + segment boundary marking) matching netconn.Conn.WriteBatch.
type crowdBatchConn struct {
	benchmarkConn
	b            *bedrock.Conn
	w            wire.Writer
	batchFn      func(*bedrock.BatchWriter) error
	pendingBatch []proto.Packet
	pendingPanic func(proto.Packet, any)
}

func (c *crowdBatchConn) WriteBatch(batch []proto.Packet, onPanic func(pk proto.Packet, r any)) error {
	if c.batchFn == nil {
		c.batchFn = c.runBatch
	}
	c.pendingBatch = batch
	c.pendingPanic = onPanic
	defer func() {
		c.pendingBatch = nil
		c.pendingPanic = nil
	}()
	return c.b.WriteBatch(c.batchFn)
}

func (c *crowdBatchConn) runBatch(bw *bedrock.BatchWriter) error {
	batch, onPanic := c.pendingBatch, c.pendingPanic
	inPersonal := false
	for i := 0; i < len(batch); {
		next, nextPersonal, err := c.writeBatchFrom(bw, batch, i, inPersonal, onPanic)
		if err != nil {
			return err
		}
		i, inPersonal = next, nextPersonal
	}
	return bw.Flush()
}

func (c *crowdBatchConn) writeBatchFrom(bw *bedrock.BatchWriter, batch []proto.Packet, start int, inPersonal bool, onPanic func(pk proto.Packet, r any)) (next int, nextPersonal bool, err error) {
	i := start
	defer func() {
		if r := recover(); r != nil {
			if onPanic != nil {
				onPanic(batch[i], r)
			}
			next, nextPersonal, err = i+1, inPersonal, nil
		}
	}()
	for ; i < len(batch); i++ {
		pk := batch[i]
		if sh, ok := pk.(*crowdShared); ok {
			raw, framed, id := sh.encoded()
			if inPersonal {
				bw.MarkSegment()
				inPersonal = false
			}
			if err = bw.WriteFramed(framed, id, len(raw)); err != nil {
				return len(batch), inPersonal, err
			}
			if ((uint64(uintptr(unsafe.Pointer(sh)))>>6)*0x9e3779b97f4a7c15)>>61 == 0 {
				bw.MarkSegment()
			}
			continue
		}
		if !inPersonal {
			bw.MarkSegment()
			inPersonal = true
		}
		c.w.B = bedrock.AppendHeader(c.w.B[:0], bedrock.Header{ID: pk.ID()})
		pk.Encode(&c.w)
		if err = bw.WritePacket(c.w.B); err != nil {
			return len(batch), inPersonal, err
		}
	}
	return len(batch), inPersonal, nil
}

func buildSessionCrowdBatches(numTicks, numSessions int) ([][][]proto.Packet, int) {
	rng := rand.New(rand.NewPCG(91, 92))
	batches := make([][][]proto.Packet, numTicks)
	rawPerConn := 0
	pad := strings.Repeat("status-hud-bar-", 14)
	for t := range numTicks {
		const sharedCount = 64
		shared := make([]proto.Packet, sharedCount)
		for k := range sharedCount {
			var pk proto.Packet
			if k%2 == 0 {
				pk = &mc.MoveActorDeltaPacket{
					EntityRuntimeID: uint64(100 + k),
					PositionX:       proto.Some(float32(rng.IntN(100)) + rng.Float32()),
					PositionY:       proto.Some(float32(64) + rng.Float32()*4),
					PositionZ:       proto.Some(float32(rng.IntN(100)) + rng.Float32()),
					RotationY:       proto.Some(rng.Float32() * 360),
				}
			} else {
				pk = &mc.SetActorMotionPacket{
					EntityRuntimeID: uint64(100 + k),
					Velocity:        [3]float32{rng.Float32()*0.2 - 0.1, -0.0784, rng.Float32()*0.2 - 0.1},
				}
			}
			shared[k] = &crowdShared{Packet: pk}
		}

		tickBatches := make([][]proto.Packet, numSessions)
		for s := range numSessions {
			batch := make([]proto.Packet, 0, sharedCount+4)
			batch = append(batch, shared...)
			batch = append(batch,
				&mc.SetTimePacket{Time: int32(t*1000 + s)},
				&mc.TextPacket{
					TextType: mc.TextTypeRaw,
					Message:  pad + string(rune('a'+(t%26))) + string(rune('a'+(s%26))),
				},
				&mc.MovePlayerPacket{
					EntityRuntimeID: 1,
					Position:        [3]float32{float32(s), 64 + float32(t%8), float32(t)},
					Tick:            uint64(t),
				},
			)
			tickBatches[s] = batch
			if t == 0 && s == 0 {
				var w wire.Writer
				for _, pk := range batch {
					if sh, ok := pk.(*crowdShared); ok {
						_, framed, _ := sh.encoded()
						rawPerConn += len(framed)
						continue
					}
					w.B = bedrock.AppendHeader(w.B[:0], bedrock.Header{ID: pk.ID()})
					pk.Encode(&w)
					rawPerConn += len(bedrock.AppendPacket(nil, w.B))
				}
			}
		}
		batches[t] = tickBatches
	}
	return batches, rawPerConn * numSessions
}

func BenchmarkSessionCrowdFlush100(b *testing.B) {
	const (
		numSessions = 100
		numTicks    = 256
	)
	batches, rawPerTick := buildSessionCrowdBatches(numTicks, numSessions)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	runMode := func(name string, comp bedrock.Compression, batchWriter bool) {
		b.Run(name, func(b *testing.B) {
			bc := bedrock.NewBatchCache()
			transports := make([]*crowdDiscardTransport, numSessions)
			sessions := make([]*Session, numSessions)
			for i := range numSessions {
				tr := &crowdDiscardTransport{}
				transports[i] = tr
				bcConn := bedrock.NewConn(tr, bedrock.Config{Cache: bc})
				bcConn.EnableCompression(comp)
				var conn Conn
				if batchWriter {
					conn = &crowdBatchConn{b: bcConn}
				} else {
					conn = &crowdWholeConn{b: bcConn}
				}
				sessions[i] = &Session{conf: Config{Log: log}, conn: conn}
			}
			b.SetBytes(int64(rawPerTick))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; b.Loop(); i++ {
				tickBatches := batches[i%numTicks]
				for s := range numSessions {
					if err := sessions[s].writeBatch(tickBatches[s]); err != nil {
						b.Fatal(err)
					}
				}
			}
			var wireTotal uint64
			for _, tr := range transports {
				wireTotal += tr.sent.Load()
			}
			b.ReportMetric(float64(wireTotal)/float64(b.N)/float64(rawPerTick), "ratio")
		})
	}

	runMode("WholeFlate", bedrock.Flate{Level: 1}, false)
	runMode("SegmentedFlate", bedrock.Flate{Level: 1}, true)
	runMode("Snappy", bedrock.Snappy{}, true)
}
