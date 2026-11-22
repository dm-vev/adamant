package bedrock

import (
	"bytes"
	"hash/maphash"
	"sync"
)

const (
	// batchCacheShards is the number of independent shards (power of two).
	batchCacheShards = 32
	// batchCacheWays is the associativity per shard (128 entries total).
	batchCacheWays = 4
	// batchCacheMaxBody skips caching single batches larger than 64 KiB so
	// rare oversized batches do not churn or bloat the cache.
	batchCacheMaxBody = 64 << 10
	// batchCacheShardMaxBytes caps retained bytes per shard (~3 MiB across
	// 32 shards), while still allowing one entry up to batchCacheMaxBody.
	batchCacheShardMaxBytes = 96 << 10
	// batchCacheShrinkAbove is the capacity threshold above which a slot's
	// backing array is replaced if it is more than twice the new entry's size.
	batchCacheShrinkAbove = 8 << 10
)

// BatchCache keeps the last batches the connections sharing it compressed,
// so that a body that many send alike, as the moves of the entities around
// a crowd on one tick, is compressed once for all of them. A batch is taken
// by the algorithm byte it starts with, whatever level of the algorithm
// compressed it, as any decompresses alike. Its methods are safe for
// concurrent use.
type BatchCache struct {
	seed   maphash.Seed
	shards [batchCacheShards]batchCacheShard
}

// batchCacheShard is a 4-way set-associative shard with its own RWMutex.
// Sized to 192 bytes (3 cache lines) on 64-bit platforms to avoid false sharing.
type batchCacheShard struct {
	mu      sync.RWMutex
	entries [batchCacheWays]batchCacheEntry
	next    uint32
	bytes   int32
}

// batchCacheEntry holds a cached body and its compressed message packed into
// a single backing slice.
type batchCacheEntry struct {
	sum     uint64
	bodyLen uint32
	alg     uint16
	data    []byte // data[:bodyLen] is body, data[bodyLen:] is msg
}

// NewBatchCache returns a BatchCache that holds nothing.
func NewBatchCache() *BatchCache { return &BatchCache{seed: maphash.MakeSeed()} }

// compress appends the batch of body compressed with alg to dst: the one a
// connection compressed alike before if the cache holds it, compressed
// now and kept otherwise.
func (bc *BatchCache) compress(dst []byte, alg Compression, body []byte) ([]byte, error) {
	a := alg.Algorithm()
	if len(body) > batchCacheMaxBody {
		return alg.Compress(append(dst, byte(a)), body)
	}
	sum := maphash.Bytes(bc.seed, body)
	s := &bc.shards[sum&(batchCacheShards-1)]
	s.mu.RLock()
	for i := range s.entries {
		e := &s.entries[i]
		if e.data != nil && e.sum == sum && e.alg == a && int(e.bodyLen) == len(body) && bytes.Equal(e.data[:e.bodyLen], body) {
			dst = append(dst, e.data[e.bodyLen:]...)
			s.mu.RUnlock()
			batchCacheHits.Inc()
			return dst, nil
		}
	}
	s.mu.RUnlock()

	start := len(dst)
	dst, err := alg.Compress(append(dst, byte(a)), body)
	if err != nil {
		return dst, err
	}
	s.store(sum, a, body, dst[start:])
	return dst, nil
}

func (s *batchCacheShard) store(sum uint64, a uint16, body, msg []byte) {
	s.mu.Lock()
	for i := range s.entries {
		e := &s.entries[i]
		if e.data != nil && e.sum == sum && e.alg == a && int(e.bodyLen) == len(body) && bytes.Equal(e.data[:e.bodyLen], body) {
			s.mu.Unlock()
			return
		}
	}
	idx := int(s.next)
	e := &s.entries[idx]
	s.bytes -= int32(cap(e.data))

	need := len(body) + len(msg)
	if cap(e.data) < need || (cap(e.data) > batchCacheShrinkAbove && cap(e.data) > need*2) {
		e.data = make([]byte, need)
	} else {
		e.data = e.data[:need]
	}
	copy(e.data, body)
	copy(e.data[len(body):], msg)
	e.sum, e.alg, e.bodyLen = sum, a, uint32(len(body))
	s.bytes += int32(cap(e.data))
	s.next = (s.next + 1) % batchCacheWays

	for step := range batchCacheWays - 1 {
		if s.bytes <= batchCacheShardMaxBytes {
			break
		}
		ev := &s.entries[(idx+1+step)%batchCacheWays]
		if ev.data != nil {
			s.bytes -= int32(cap(ev.data))
			*ev = batchCacheEntry{}
		}
	}
	s.mu.Unlock()
}
