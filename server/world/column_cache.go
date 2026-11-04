package world

// SlotKey names a set of encoded payloads of columns that an adapter keeps
// apart from the others in the caches of columns: those of the clients that
// name blocks alike, for one. A column keeps the payloads of each key it is
// asked for apart, and each change of the column clears those of every key
// alike, as it clears its own. Keys compare by pointer: make one with
// NewSlotKey for each set and keep it.
type SlotKey struct{ name string }

// NewSlotKey returns a new key, of a name that names it in messages alone.
func NewSlotKey(name string) *SlotKey { return &SlotKey{name: name} }

// String returns the name of the key.
func (k *SlotKey) String() string { return k.name }

// ColumnCache holds the encoded payloads of a column of one SlotKey that
// depend on how blocks are named: those of its sub-chunks and of the whole
// chunk. Those of biomes, block entities and height maps, which do not, are
// the column's alone (Column.CachedNetworkBiomePayload and the others).
// A ColumnCache is used as the column it is of is, by the owner of its
// world.
type ColumnCache struct {
	subChunks map[int16]cachedBlockEntityPayload
	full      cachedBlockEntityPayload
}

// Cache returns the cache of the column for a key, made the first time;
// that of nil is the one the methods of the column itself fill, such as
// CacheNetworkSubChunkPayload.
func (c *Column) Cache(k *SlotKey) *ColumnCache {
	if k == nil {
		return &c.ownCache
	}
	if cache, ok := c.caches[k]; ok {
		return cache
	}
	if c.caches == nil {
		c.caches = make(map[*SlotKey]*ColumnCache, 1)
	}
	cache := &ColumnCache{}
	c.caches[k] = cache
	return cache
}

// CachedNetworkSubChunkPayload returns the cached encoded network payload
// of a sub-chunk.
func (c *ColumnCache) CachedNetworkSubChunkPayload(ind int16) ([]byte, bool) {
	payload, ok := c.subChunks[ind]
	return payload.payload, ok && payload.ready
}

// CacheNetworkSubChunkPayload stores the encoded network payload of a
// sub-chunk.
func (c *ColumnCache) CacheNetworkSubChunkPayload(ind int16, payload []byte) {
	if c.subChunks == nil {
		c.subChunks = make(map[int16]cachedBlockEntityPayload, 4)
	}
	c.subChunks[ind] = cachedBlockEntityPayload{payload: payload, ready: true}
}

// CachedFullChunkPayload returns the cached full chunk raw payload.
func (c *ColumnCache) CachedFullChunkPayload() ([]byte, bool) {
	return c.full.payload, c.full.ready
}

// CacheFullChunkPayload stores the full chunk raw payload.
func (c *ColumnCache) CacheFullChunkPayload(payload []byte) {
	c.full = cachedBlockEntityPayload{payload: payload, ready: true}
}

// invalidate clears the payload of the chunk, and those of the sub-chunks
// if subChunks is set.
func (c *ColumnCache) invalidate(subChunks bool) {
	if subChunks {
		clear(c.subChunks)
	}
	c.full = cachedBlockEntityPayload{}
}
