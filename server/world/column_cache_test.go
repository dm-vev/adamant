package world

import "testing"

// TestColumnCachesOfKeysAreInvalidated fills the payloads of a column of
// no key and of two keys and checks each change of the column clears what
// it clears of its own cache in the cache of every key alike: a change of
// blocks the sub-chunks and the chunk, a change of biomes or block
// entities the chunk alone.
func TestColumnCachesOfKeysAreInvalidated(t *testing.T) {
	a, b := NewSlotKey("a"), NewSlotKey("b")
	if a.String() != "a" || a == NewSlotKey("a") {
		t.Fatal("keys of a name are not their own")
	}
	for _, c := range []struct {
		name       string
		invalidate func(*Column)
		subChunks  bool
	}{
		{"blocks", (*Column).invalidateNetworkSubChunkPayloads, false},
		{"biomes", (*Column).invalidateNetworkBiomePayload, true},
		{"block entities", (*Column).invalidateBlockEntityPayloads, true},
	} {
		col := &Column{}
		caches := []*ColumnCache{col.Cache(nil), col.Cache(a), col.Cache(b)}
		if caches[0] != &col.ownCache || col.Cache(a) != caches[1] || caches[1] == caches[2] {
			t.Fatal("a key does not name a cache of its own")
		}
		for i, cache := range caches {
			cache.CacheNetworkSubChunkPayload(3, []byte{byte(i)})
			cache.CacheFullChunkPayload([]byte{byte(i), 1})
		}
		if p, ok := col.CachedNetworkSubChunkPayload(3); !ok || p[0] != 0 {
			t.Fatal("the column does not read its own cache")
		}
		if p, ok := caches[2].CachedFullChunkPayload(); !ok || p[0] != 2 {
			t.Fatal("a key does not read its own cache")
		}
		c.invalidate(col)
		for i, cache := range caches {
			if _, ok := cache.CachedFullChunkPayload(); ok {
				t.Errorf("%s: the chunk of cache %d is kept", c.name, i)
			}
			if _, ok := cache.CachedNetworkSubChunkPayload(3); ok != c.subChunks {
				t.Errorf("%s: the sub-chunk of cache %d kept: %v, want %v", c.name, i, ok, c.subChunks)
			}
		}
	}
}
