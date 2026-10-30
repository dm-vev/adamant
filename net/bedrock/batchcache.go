package bedrock

import (
	"bytes"
	"hash/maphash"
	"sync"
)

// batchCacheEntries is the number of batches a BatchCache keeps.
const batchCacheEntries = 64

// BatchCache keeps the last batches the connections sharing it compressed,
// so that a body that many send alike, as the moves of the entities around
// a crowd on one tick, is compressed once for all of them. A batch is taken
// by the algorithm byte it starts with, whatever level of the algorithm
// compressed it, as any decompresses alike. Its methods are safe for
// concurrent use.
type BatchCache struct {
	seed    maphash.Seed
	mu      sync.Mutex
	entries [batchCacheEntries]batchCacheEntry
	next    int
}

// batchCacheEntry is a body, the compression it was compressed with, its
// hash and the batch compressed.
type batchCacheEntry struct {
	alg       uint16
	sum       uint64
	body, msg []byte
}

// NewBatchCache returns a BatchCache that holds nothing.
func NewBatchCache() *BatchCache { return &BatchCache{seed: maphash.MakeSeed()} }

// compress appends the batch of body compressed with alg to dst: the one a
// connection compressed alike before if the cache holds it, compressed
// now and kept otherwise.
func (bc *BatchCache) compress(dst []byte, alg Compression, body []byte) ([]byte, error) {
	sum, a := maphash.Bytes(bc.seed, body), alg.Algorithm()
	bc.mu.Lock()
	for i := range bc.entries {
		e := &bc.entries[i]
		if e.sum == sum && e.alg == a && len(e.body) == len(body) && e.msg != nil && bytes.Equal(e.body, body) {
			// Copied under the lock: the entry may be taken by another body
			// once it is released.
			dst = append(dst, e.msg...)
			bc.mu.Unlock()
			batchCacheHits.Inc()
			return dst, nil
		}
	}
	bc.mu.Unlock()
	start := len(dst)
	dst, err := alg.Compress(append(dst, byte(a)), body)
	if err != nil {
		return dst, err
	}
	bc.mu.Lock()
	e := &bc.entries[bc.next]
	bc.next = (bc.next + 1) % batchCacheEntries
	e.alg, e.sum = a, sum
	e.body = append(e.body[:0], body...)
	e.msg = append(e.msg[:0], dst[start:]...)
	bc.mu.Unlock()
	return dst, nil
}
