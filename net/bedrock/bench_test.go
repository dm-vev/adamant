package bedrock

import (
	"bytes"
	"encoding/binary"
	"io"
	"math/rand/v2"
	"sync/atomic"
	"testing"
)

// discardTransport is a zero-allocation MessageConn for write benchmarks.
type discardTransport struct {
	sent atomic.Uint64
}

func (d *discardTransport) ReadMessage() ([]byte, error) { return nil, io.EOF }
func (d *discardTransport) Send(msg []byte) error {
	d.sent.Add(uint64(len(msg)))
	return nil
}
func (d *discardTransport) Close() error { return nil }

// loopTransport replays a single pre-encoded message for read benchmarks.
type loopTransport struct {
	msg []byte
}

func (l *loopTransport) ReadMessage() ([]byte, error) { return l.msg, nil }
func (l *loopTransport) Send([]byte) error            { return nil }
func (l *loopTransport) Close() error                 { return nil }

// fastSnappyAlg is a minimal Compression with AlgorithmSnappy used to measure
// BatchCache overhead independent of flate CPU cost.
type fastSnappyAlg struct{}

func (fastSnappyAlg) Algorithm() uint16 { return AlgorithmSnappy }
func (fastSnappyAlg) Compress(dst, src []byte) ([]byte, error) {
	// Store length-prefixed literal block (minimal fast-path cost).
	dst = binary.AppendUvarint(dst, uint64(len(src)))
	return append(dst, src...), nil
}
func (fastSnappyAlg) Decompress(dst, src []byte, limit int) ([]byte, error) {
	n, k := binary.Uvarint(src)
	if k <= 0 || int(n) > limit || len(src)-k < int(n) {
		return nil, ErrTooLarge
	}
	return append(dst, src[k:k+int(n)]...), nil
}

// chunkBatch returns a synthetic Bedrock chunk batch of approximately size
// bytes with realistic repetition (subchunk palette runs + biome arrays + noise).
func chunkBatch(rng *rand.Rand, size int) []byte {
	raw := make([]byte, 0, size)
	// Repeated stone/dirt/air palette runs interleaved with varied sections.
	patterns := [][]byte{
		bytes.Repeat([]byte{0x02, 0x00, 0x01, 0x00}, 16),
		bytes.Repeat([]byte{0x00, 0x00, 0x00, 0x00}, 24),
		bytes.Repeat([]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88}, 8),
	}
	for len(raw) < size {
		switch rng.IntN(4) {
		case 0, 1:
			raw = append(raw, patterns[rng.IntN(len(patterns))]...)
		case 2:
			var tmp [32]byte
			for i := range tmp {
				tmp[i] = byte(rng.Uint32() & 0x3f)
			}
			raw = append(raw, tmp[:]...)
		default:
			var tmp [48]byte
			for i := range tmp {
				tmp[i] = byte(rng.Uint32())
			}
			raw = append(raw, tmp[:]...)
		}
	}
	raw = raw[:size]
	pk := packet(58, raw) // LevelChunk-like packet
	return AppendPacket(nil, pk)
}

func makeSmallBatches(rng *rand.Rand, count, bodySize int) [][]byte {
	out := make([][]byte, count)
	for i := range out {
		b := tickBatch(rng)
		if len(b) > bodySize {
			b = b[:bodySize]
		}
		out[i] = b
	}
	return out
}

func makeBenchBatches(seed1, seed2 uint64) (small, ticks, chunks8, chunks24 [][]byte) {
	rng := rand.New(rand.NewPCG(seed1, seed2))
	small = makeSmallBatches(rng, 32, 320)
	ticks = make([][]byte, 32)
	for i := range ticks {
		ticks[i] = tickBatch(rng)
	}
	chunks8 = make([][]byte, 16)
	chunks24 = make([][]byte, 16)
	for i := range chunks8 {
		chunks8[i] = chunkBatch(rng, 8<<10)
		chunks24[i] = chunkBatch(rng, 24<<10)
	}
	return small, ticks, chunks8, chunks24
}

func benchAlgCompress(b *testing.B, name string, comp Compression, inputs [][]byte, parallel bool) {
	b.Run(name, func(b *testing.B) {
		totalIn := 0
		for _, in := range inputs {
			totalIn += len(in)
		}
		avgIn := totalIn / len(inputs)
		b.SetBytes(int64(avgIn))
		b.ReportAllocs()
		if parallel {
			var idx atomic.Uint64
			b.RunParallel(func(pb *testing.PB) {
				var dst []byte
				for pb.Next() {
					in := inputs[idx.Add(1)%uint64(len(inputs))]
					var err error
					dst, err = comp.Compress(dst[:0], in)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
			return
		}
		var dst []byte
		totalOut := 0
		for i := 0; b.Loop(); i++ {
			var err error
			dst, err = comp.Compress(dst[:0], inputs[i%len(inputs)])
			if err != nil {
				b.Fatal(err)
			}
			totalOut += len(dst)
		}
		b.ReportMetric(float64(totalOut)/float64(b.N)/float64(avgIn), "ratio")
	})
}

func benchAlgDecompress(b *testing.B, name string, comp Compression, inputs [][]byte) {
	compressed := make([][]byte, len(inputs))
	totalIn := 0
	for i, in := range inputs {
		c, err := comp.Compress(nil, in)
		if err != nil {
			b.Fatal(err)
		}
		compressed[i] = c
		totalIn += len(in)
	}
	b.Run(name, func(b *testing.B) {
		b.SetBytes(int64(totalIn / len(inputs)))
		b.ReportAllocs()
		for i := 0; b.Loop(); i++ {
			out, err := comp.Decompress(nil, compressed[i%len(compressed)], 1<<20)
			if err != nil || len(out) == 0 {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkFlate(b *testing.B) {
	small, ticks, chunks8, chunks24 := makeBenchBatches(11, 12)
	benchAlgCompress(b, "Compress/Small320B", Flate{}, small, false)
	benchAlgCompress(b, "Compress/Tick1850B", Flate{}, ticks, false)
	benchAlgCompress(b, "Compress/Chunk8KiB_Fixed", Flate{}, chunks8, false)
	benchAlgCompress(b, "Compress/Chunk24KiB_Stdlib", Flate{}, chunks24, false)
	benchAlgCompress(b, "Parallel/Tick1850B_Fixed", Flate{}, ticks, true)
	benchAlgCompress(b, "Parallel/Chunk24KiB_Stdlib", Flate{}, chunks24, true)
	benchAlgDecompress(b, "Decompress/Tick1850B", Flate{}, ticks)
	benchAlgDecompress(b, "Decompress/Chunk24KiB", Flate{}, chunks24)
}

func BenchmarkSnappy(b *testing.B) {
	small, ticks, chunks8, chunks24 := makeBenchBatches(11, 12)
	var sn Snappy
	benchAlgCompress(b, "Compress/Small320B", sn, small, false)
	benchAlgCompress(b, "Compress/Tick1850B", sn, ticks, false)
	benchAlgCompress(b, "Compress/Chunk8KiB", sn, chunks8, false)
	benchAlgCompress(b, "Compress/Chunk24KiB", sn, chunks24, false)
	benchAlgCompress(b, "Parallel/Tick1850B", sn, ticks, true)
	benchAlgCompress(b, "Parallel/Chunk24KiB", sn, chunks24, true)
	benchAlgDecompress(b, "Decompress/Tick1850B", sn, ticks)
	benchAlgDecompress(b, "Decompress/Chunk24KiB", sn, chunks24)
}

type crowdPacket struct {
	raw    []byte
	framed []byte
	id     uint32
	cut    bool
}

func buildCrowdTicks(numTicks, numPlayers int) (shared [][]crowdPacket, personal [][][]byte, rawBytesPerTick int) {
	rng := rand.New(rand.NewPCG(71, 72))
	shared = make([][]crowdPacket, numTicks)
	personal = make([][][]byte, numTicks)
	for t := range numTicks {
		tickBody := tickBatch(rng)
		var pks []crowdPacket
		idx := 0
		_ = Packets(tickBody, 1024, func(raw []byte) error {
			id, _ := packetID(raw)
			cp := bytes.Clone(raw)
			pks = append(pks, crowdPacket{
				raw:    cp,
				framed: AppendPacket(nil, cp),
				id:     uint32(id),
				cut:    idx%12 == 11,
			})
			if t == 0 {
				rawBytesPerTick += len(pks[len(pks)-1].framed)
			}
			idx++
			return nil
		})
		shared[t] = pks

		perTick := make([][]byte, numPlayers)
		for p := range numPlayers {
			per := make([]byte, 240)
			for k := range per {
				per[k] = byte(rng.Uint32())
			}
			pk := packet(9, per)
			if t == 0 && p == 0 {
				rawBytesPerTick += len(AppendPacket(nil, pk))
			}
			perTick[p] = pk
		}
		personal[t] = perTick
	}
	return shared, personal, rawBytesPerTick * numPlayers
}

func BenchmarkFlateSegmentedCrowd100(b *testing.B) {
	const (
		numPlayers = 100
		numTicks   = 256
	)
	shared, personal, rawPerTick := buildCrowdTicks(numTicks, numPlayers)

	runMode := func(name string, comp Compression, segmented bool) {
		b.Run(name, func(b *testing.B) {
			bc := NewBatchCache()
			transports := make([]*discardTransport, numPlayers)
			conns := make([]*Conn, numPlayers)
			for i := range numPlayers {
				transports[i] = &discardTransport{}
				conns[i] = NewConn(transports[i], Config{Cache: bc})
				conns[i].EnableCompression(comp)
			}
			b.SetBytes(int64(rawPerTick))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; b.Loop(); i++ {
				tIdx := i % numTicks
				sh := shared[tIdx]
				per := personal[tIdx]
				for p := range numPlayers {
					c := conns[p]
					if err := c.WriteBatch(func(bw *BatchWriter) error {
						for k := range sh {
							if err := bw.WriteFramed(sh[k].framed, sh[k].id, len(sh[k].raw)); err != nil {
								return err
							}
							if segmented && sh[k].cut {
								bw.MarkSegment()
							}
						}
						if segmented {
							bw.MarkSegment()
						}
						if err := bw.WritePacket(per[p]); err != nil {
							return err
						}
						return bw.Flush()
					}); err != nil {
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

	runMode("WholeFlate", Flate{Level: 1}, false)
	runMode("SegmentedFlate", Flate{Level: 1}, true)
	runMode("Snappy", Snappy{}, false)
}

func BenchmarkBatchCache(b *testing.B) {
	rng := rand.New(rand.NewPCG(21, 22))
	sharedTicks := make([][]byte, 16)
	for i := range sharedTicks {
		sharedTicks[i] = tickBatch(rng)
	}
	uniqueTicks := make([][]byte, 256)
	for i := range uniqueTicks {
		uniqueTicks[i] = tickBatch(rng)
	}
	sharedChunks := make([][]byte, 16)
	for i := range sharedChunks {
		sharedChunks[i] = chunkBatch(rng, 12<<10)
	}
	uniqueChunks := make([][]byte, 128)
	for i := range uniqueChunks {
		uniqueChunks[i] = chunkBatch(rng, 12<<10)
	}

	run := func(name string, alg Compression, shared, unique [][]byte, hitPct int) {
		b.Run(name, func(b *testing.B) {
			bc := NewBatchCache()
			// Warm shared entries.
			for _, s := range shared {
				if _, err := bc.compress(nil, alg, s); err != nil {
					b.Fatal(err)
				}
			}
			avgSize := len(shared[0])
			b.SetBytes(int64(avgSize))
			b.ReportAllocs()
			b.ResetTimer()
			var seq atomic.Uint64
			b.RunParallel(func(pb *testing.PB) {
				dst := make([]byte, 0, avgSize+avgSize/4+64)
				for pb.Next() {
					n := seq.Add(1)
					var body []byte
					if int(n%100) < hitPct {
						body = shared[n%uint64(len(shared))]
					} else {
						body = unique[n%uint64(len(unique))]
					}
					var err error
					dst, err = bc.compress(dst[:0], alg, body)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}

	run("Parallel_Hit100_Tick2KiB_Flate", Flate{}, sharedTicks, uniqueTicks, 100)
	run("Parallel_Hit80_Tick2KiB_Flate", Flate{}, sharedTicks, uniqueTicks, 80)
	run("Parallel_Hit50_Chunk12KiB_Flate", Flate{}, sharedChunks, uniqueChunks, 50)
	run("Parallel_Miss100_Tick2KiB_Flate", Flate{}, sharedTicks, uniqueTicks, 0)
	run("Parallel_Hit80_Tick2KiB_SnappyAlg", fastSnappyAlg{}, sharedTicks, uniqueTicks, 80)
	run("Parallel_Hit100_Tick2KiB_SnappyAlg", fastSnappyAlg{}, sharedTicks, uniqueTicks, 100)
}

func BenchmarkConn(b *testing.B) {
	rng := rand.New(rand.NewPCG(31, 32))
	// Build realistic packets for a tick (~20 packets, ~1.5 KiB total).
	tickPackets := make([][]byte, 20)
	tickBytes := 0
	for i := range tickPackets {
		payload := make([]byte, 64+rng.IntN(32))
		for j := range payload {
			payload[j] = byte(rng.Uint32())
		}
		tickPackets[i] = packet(uint32(18+i%5), payload)
		tickBytes += len(tickPackets[i])
	}
	uniqueTickPackets := make([][][]byte, 64)
	for g := range uniqueTickPackets {
		pks := make([][]byte, len(tickPackets))
		for i, p := range tickPackets {
			cp := bytes.Clone(p)
			cp[len(cp)-1] ^= byte(g)
			cp[len(cp)-2] ^= byte(g * 7)
			pks[i] = cp
		}
		uniqueTickPackets[g] = pks
	}

	b.Run("WriteFlush_Parallel_64Players_SharedCache", func(b *testing.B) {
		bc := NewBatchCache()
		conns := make([]*Conn, 64)
		for i := range conns {
			c := NewConn(&discardTransport{}, Config{Cache: bc})
			c.EnableCompression(Flate{})
			conns[i] = c
		}
		b.SetBytes(int64(tickBytes))
		b.ReportAllocs()
		var seq atomic.Uint64
		b.RunParallel(func(pb *testing.PB) {
			id := int(seq.Add(1)-1) % len(conns)
			// Dedicated Conn per goroutine would mirror 1 goroutine per player;
			// wrap with unique index to avoid blocking on same conn writeMu.
			c := NewConn(&discardTransport{}, Config{Cache: bc})
			c.EnableCompression(Flate{})
			for i := 0; pb.Next(); i++ {
				pks := tickPackets
				if (i+id)%5 == 0 {
					pks = uniqueTickPackets[(i+id)%len(uniqueTickPackets)]
				}
				for _, pk := range pks {
					if err := c.WritePacket(pk); err != nil {
						b.Fatal(err)
					}
				}
				if err := c.Flush(); err != nil {
					b.Fatal(err)
				}
			}
		})
	})

	b.Run("ReadPacket_TickBatch", func(b *testing.B) {
		msg, err := EncodeBatch(Flate{}, tickPackets...)
		if err != nil {
			b.Fatal(err)
		}
		c := NewConn(&loopTransport{msg: msg}, Config{})
		c.EnableCompression(Flate{})
		b.SetBytes(int64(tickBytes))
		b.ReportAllocs()
		for b.Loop() {
			for range len(tickPackets) {
				if _, err := c.ReadPacket(); err != nil {
					b.Fatal(err)
				}
			}
		}
	})

	b.Run("HandleBatch_TickBatch", func(b *testing.B) {
		msg, err := EncodeBatch(Flate{}, tickPackets...)
		if err != nil {
			b.Fatal(err)
		}
		c := NewConn(&discardTransport{}, Config{})
		c.EnableCompression(Flate{})
		b.SetBytes(int64(tickBytes))
		b.ReportAllocs()
		for b.Loop() {
			if err := c.HandleBatch(msg, func([]byte) error { return nil }); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("EncodeBatch_20Packets", func(b *testing.B) {
		b.SetBytes(int64(tickBytes))
		b.ReportAllocs()
		for b.Loop() {
			if _, err := EncodeBatch(Flate{}, tickPackets...); err != nil {
				b.Fatal(err)
			}
		}
	})
}
