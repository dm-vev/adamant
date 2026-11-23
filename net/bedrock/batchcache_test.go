package bedrock

import (
	"bytes"
	"fmt"
	"sync"
	"testing"
)

// TestBatchCache sends the same batch on connections sharing a cache: it
// goes out alike on each, compressed once, and reads back on the other end,
// on a connection that compresses at another level of the algorithm too,
// as either decompresses alike; a batch of another body is compressed anew.
func TestBatchCache(t *testing.T) {
	cache := NewBatchCache()
	moves := packet(111, bytes.Repeat([]byte("move of an entity "), 40))
	other := packet(111, bytes.Repeat([]byte("another move "), 40))
	var sent [][]byte
	before := batchCacheHits.Load()
	for i := range 4 {
		a, b := pipes()
		ca, cb := NewConn(a, Config{Cache: cache}), NewConn(b, Config{})
		var comp Compression = Flate{Level: 1}
		if i == 3 {
			comp = Flate{Level: 9}
		}
		ca.EnableCompression(comp)
		cb.EnableCompression(comp)
		_ = ca.WritePacket(moves)
		_ = ca.Flush()
		_ = ca.WritePacket(other)
		_ = ca.Flush()
		sent = append(sent, a.sent[0])
		for _, want := range [][]byte{moves, other} {
			if p, err := cb.ReadPacket(); err != nil || !bytes.Equal(p, want) {
				t.Fatalf("connection %d read %d bytes, %v; want %d", i, len(p), err, len(want))
			}
		}
	}
	for i := 1; i < 4; i++ {
		if !bytes.Equal(sent[i], sent[0]) {
			t.Fatalf("connection %d sent another batch for the same body", i)
		}
	}
	// Four connections share the two bodies: six of their eight batches
	// come from the cache.
	if hits := batchCacheHits.Load() - before; hits != 6 {
		t.Fatalf("%d batches from the cache, want 6", hits)
	}
}

// TestBatchCacheConcurrent compresses bodies on many goroutines at once
// with one cache: each gets its own batch back whole.
func TestBatchCacheConcurrent(t *testing.T) {
	cache := NewBatchCache()
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 500 {
				body := packet(111, []byte(fmt.Sprintf("body %d of %d %s", i%100, g%2, bytes.Repeat([]byte{byte(i % 100)}, 300))))
				msg, err := cache.compress(nil, Flate{Level: 1}, body)
				if err != nil {
					t.Error(err)
					return
				}
				got, err := Flate{}.Decompress(nil, msg[1:], 1<<20)
				if err != nil || !bytes.Equal(got, body) {
					t.Errorf("body %d decompressed to %d bytes, %v", i, len(got), err)
					return
				}
			}
		})
	}
	wg.Wait()
}

// TestPacketsCountedPerBatch writes packets of a few IDs: once the batch
// goes, the counters of each ID hold its packets and bytes, as when they
// were counted a packet at a time.
func TestPacketsCountedPerBatch(t *testing.T) {
	a, _ := pipes()
	c := NewConn(a, Config{})
	c.EnableCompression(Flate{})
	moves, text := packet(111, []byte("move")), packet(9, []byte("hello"))
	m0, mb0, t0 := packetsSent.At(111).Load(), packetBytesSent.At(111).Load(), packetsSent.At(9).Load()
	for range 5 {
		_ = c.WritePacket(moves)
	}
	_ = c.WritePacket(text)
	_ = c.Flush()
	if got := packetsSent.At(111).Load() - m0; got != 5 {
		t.Fatalf("%d moves counted, want 5", got)
	}
	if got := packetBytesSent.At(111).Load() - mb0; got != uint64(5*len(moves)) {
		t.Fatalf("%d bytes of moves counted, want %d", got, 5*len(moves))
	}
	if got := packetsSent.At(9).Load() - t0; got != 1 {
		t.Fatalf("%d texts counted, want 1", got)
	}
}

// TestBatchCacheMemoryBounded checks that large batches stay within the per-shard
// memory cap and that replacing them with small batches shrinks retained buffers
// rather than pinning huge backing arrays forever.
func TestBatchCacheMemoryBounded(t *testing.T) {
	cache := NewBatchCache()
	retained := func() int {
		total := 0
		for i := range cache.shards {
			s := &cache.shards[i]
			s.mu.RLock()
			for j := range s.entries {
				total += cap(s.entries[j].data)
			}
			s.mu.RUnlock()
		}
		return total
	}

	// Fill with 128 large (48 KiB) distinct bodies.
	for i := range 128 {
		body := bytes.Repeat([]byte{byte(i), byte(i >> 3), 0x5a, 0xa5}, 12<<10)
		if _, err := cache.compress(nil, Flate{}, body); err != nil {
			t.Fatal(err)
		}
	}
	peak := retained()
	// Worst case: 32 shards * (96 KiB + headroom for 1 large entry) <= 4 MiB.
	const maxPeak = 4 << 20
	if peak == 0 || peak > maxPeak {
		t.Fatalf("retained %d bytes after large batches, want in (0, %d]", peak, maxPeak)
	}

	// Oversized batch (> batchCacheMaxBody) bypasses cache cleanly.
	huge := bytes.Repeat([]byte("huge-batch-data!"), (batchCacheMaxBody/16)+1024)
	msg, err := cache.compress(nil, Flate{}, huge)
	if err != nil {
		t.Fatal(err)
	}
	got, err := (Flate{}).Decompress(nil, msg[1:], len(huge))
	if err != nil || !bytes.Equal(got, huge) {
		t.Fatalf("oversized batch round-trip failed: len=%d err=%v", len(got), err)
	}

	// Replace cache contents with 512 small (400 B) distinct bodies; oversized
	// backing arrays must be dropped and total retained memory must shrink.
	for i := range 512 {
		body := packet(111, []byte(fmt.Sprintf("small-tick-%04d-%s", i, bytes.Repeat([]byte{byte(i)}, 360))))
		if _, err := cache.compress(nil, Flate{}, body); err != nil {
			t.Fatal(err)
		}
	}
	after := retained()
	if after > 256<<10 {
		t.Fatalf("retained %d bytes after small batches (peak %d), oversized backing arrays were not released", after, peak)
	}
}

// TestBatchCacheAlgorithmsAndStress verifies algorithm isolation and mixed-size
// concurrent access under the race detector.
func TestBatchCacheAlgorithmsAndStress(t *testing.T) {
	cache := NewBatchCache()
	body := packet(111, bytes.Repeat([]byte("shared payload "), 60))

	flateMsg, err := cache.compress(nil, Flate{}, body)
	if err != nil || flateMsg[0] != byte(AlgorithmFlate) {
		t.Fatalf("flate compress: prefix=%v err=%v", flateMsg[:1], err)
	}
	snappyMsg, err := cache.compress(nil, fastSnappyAlg{}, body)
	if err != nil || snappyMsg[0] != byte(AlgorithmSnappy) {
		t.Fatalf("snappy compress: prefix=%v err=%v", snappyMsg[:1], err)
	}
	if bytes.Equal(flateMsg, snappyMsg) {
		t.Fatal("different algorithms collided in BatchCache")
	}

	var wg sync.WaitGroup
	for g := range 12 {
		wg.Go(func() {
			var dst []byte
			for i := range 200 {
				var b []byte
				switch i % 4 {
				case 0:
					b = body
				case 1:
					b = packet(111, bytes.Repeat([]byte{byte(i % 16), byte(g % 3)}, 300))
				case 2:
					b = packet(58, bytes.Repeat([]byte{byte(i % 8), 1, 2, 3}, 4096))
				case 3:
					b = packet(58, bytes.Repeat([]byte{byte(i), byte(g)}, 20000))
				}
				var alg Compression = Flate{}
				if (i+g)%3 == 0 {
					alg = fastSnappyAlg{}
				}
				var err error
				dst, err = cache.compress(dst[:0], alg, b)
				if err != nil {
					t.Error(err)
					return
				}
				got, err := alg.Decompress(nil, dst[1:], 1<<20)
				if err != nil || !bytes.Equal(got, b) {
					t.Errorf("g=%d i=%d alg=%d mismatch: len=%d want=%d err=%v", g, i, alg.Algorithm(), len(got), len(b), err)
					return
				}
			}
		})
	}
	wg.Wait()
}

func TestBatchCacheSegmentedWriteBatch(t *testing.T) {
	cache := NewBatchCache()
	shared1 := packet(18, bytes.Repeat([]byte("shared-crowd-seg-1 "), 18))
	shared2 := packet(18, bytes.Repeat([]byte("shared-crowd-seg-2 "), 18))
	personalA := packet(9, bytes.Repeat([]byte("personal-alpha "), 20))
	personalB := packet(9, bytes.Repeat([]byte("personal-bravo "), 20))

	// Check algFlateSegment isolation from AlgorithmFlate for identical payload.
	fullMsg, err := cache.compress(nil, Flate{}, shared1)
	if err != nil {
		t.Fatal(err)
	}
	segMsg, err := cache.compressSegment(nil, Flate{}, shared1)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(fullMsg[1:], segMsg) {
		t.Fatal("segment compressed bytes collided with full-body entry")
	}

	a1, b1 := pipes()
	a2, b2 := pipes()
	c1, r1 := NewConn(a1, Config{Cache: cache}), NewConn(b1, Config{})
	c2, r2 := NewConn(a2, Config{Cache: cache}), NewConn(b2, Config{})
	for _, c := range []*Conn{c1, r1, c2, r2} {
		c.EnableCompression(Flate{})
	}

	before := batchCacheHits.Load()
	writeCrowd := func(c *Conn, personal []byte) {
		err := c.WriteBatch(func(bw *BatchWriter) error {
			// < 256B MarkSegment is ignored; >= 256B records boundary.
			if err := bw.WritePacket(personal[:20]); err != nil {
				return err
			}
			bw.MarkSegment() // ignored (<256B)
			if err := bw.WriteFramed(AppendPacket(nil, shared1), 18, len(shared1)); err != nil {
				return err
			}
			bw.MarkSegment() // recorded (>=256B)
			if err := bw.WriteFramed(AppendPacket(nil, shared2), 18, len(shared2)); err != nil {
				return err
			}
			bw.MarkSegment() // recorded (>=256B)
			if err := bw.WritePacket(personal); err != nil {
				return err
			}
			return bw.Flush()
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	// First connection misses segments (note first seg includes personalA[:20],
	// while shared2 is purely shared!).
	writeCrowd(c1, personalA)
	// Second connection misses personal-mixed first seg & personal tail, hits shared2!
	writeCrowd(c2, personalB)
	if hits := batchCacheHits.Load() - before; hits != 1 {
		t.Fatalf("hits = %d, want 1 (shared2 segment)", hits)
	}

	// Third batch with identical body to c1 hits full-body or all segments.
	writeCrowd(c1, personalA)
	if hits := batchCacheHits.Load() - before; hits < 4 {
		t.Fatalf("expected segment hits on replay, got %d total hits", hits)
	}

	for _, tc := range []struct {
		r        *Conn
		personal []byte
	}{
		{r1, personalA},
		{r2, personalB},
		{r1, personalA},
	} {
		for _, want := range [][]byte{tc.personal[:20], shared1, shared2, tc.personal} {
			got, err := tc.r.ReadPacket()
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("read len=%d err=%v, want len=%d", len(got), err, len(want))
			}
		}
	}
}

func TestConnWriteBatchFallbacksAndFullBodyHit(t *testing.T) {
	cache := NewBatchCache()
	p1 := packet(18, bytes.Repeat([]byte("seg-fallback-1 "), 22))
	p2 := packet(18, bytes.Repeat([]byte("seg-fallback-2 "), 22))

	// 1. Pre-populate full-body cache hit; WriteBatch checks full-body first.
	body := AppendPacket(AppendPacket(nil, p1), p2)
	cachedFull, err := cache.compress(nil, Flate{}, body)
	if err != nil {
		t.Fatal(err)
	}
	a, b := pipes()
	ca, cb := NewConn(a, Config{Cache: cache}), NewConn(b, Config{})
	ca.EnableCompression(Flate{})
	cb.EnableCompression(Flate{})

	before := batchCacheHits.Load()
	if err := ca.WriteBatch(func(bw *BatchWriter) error {
		_ = bw.WritePacket(p1)
		bw.MarkSegment()
		_ = bw.WritePacket(p2)
		return bw.Flush()
	}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.sent[0], cachedFull) || batchCacheHits.Load()-before != 1 {
		t.Fatalf("expected full-body cache hit; hits=%d", batchCacheHits.Load()-before)
	}
	for _, want := range [][]byte{p1, p2} {
		if got, err := cb.ReadPacket(); err != nil || !bytes.Equal(got, want) {
			t.Fatalf("full-body hit read failed: err=%v", err)
		}
	}

	// 2. Segment >= FixedBelow falls back to whole-body compression cleanly.
	a2, b2 := pipes()
	ca2, cb2 := NewConn(a2, Config{Cache: cache}), NewConn(b2, Config{})
	ca2.EnableCompression(Flate{FixedBelow: 200})
	cb2.EnableCompression(Flate{})
	if err := ca2.WriteBatch(func(bw *BatchWriter) error {
		_ = bw.WritePacket(p1)
		bw.MarkSegment()
		_ = bw.WritePacket(p2)
		return bw.Flush()
	}); err != nil {
		t.Fatal(err)
	}
	for _, want := range [][]byte{p1, p2} {
		if got, err := cb2.ReadPacket(); err != nil || !bytes.Equal(got, want) {
			t.Fatalf("fixedBelow fallback read failed: err=%v", err)
		}
	}
}
