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
