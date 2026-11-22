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
	col       *Column
	subChunks map[int16]cachedBlockEntityPayload
	full      cachedBlockEntityPayload
}

// Cache returns the cache of the column for a key, made the first time;
// that of nil is the one the methods of the column itself fill, such as
// CacheNetworkSubChunkPayload.
func (c *Column) Cache(k *SlotKey) *ColumnCache {
	if k == nil {
		if c.ownCache.col == nil {
			c.ownCache.col = c
		}
		return &c.ownCache
	}
	if cache, ok := c.caches[k]; ok {
		return cache
	}
	if c.caches == nil {
		c.caches = make(map[*SlotKey]*ColumnCache, 1)
	}
	cache := &ColumnCache{col: c}
	c.caches[k] = cache
	return cache
}

// CachedNetworkSubChunkPayload returns the cached encoded network payload
// of a sub-chunk.
func (c *ColumnCache) CachedNetworkSubChunkPayload(ind int16) ([]byte, bool) {
	payload, ok := c.subChunks[ind]
	if ok && payload.ready {
		if c.col != nil {
			c.col.touchWireCache()
		}
		return payload.payload, true
	}
	return payload.payload, false
}

// CacheNetworkSubChunkPayload stores the encoded network payload of a
// sub-chunk.
func (c *ColumnCache) CacheNetworkSubChunkPayload(ind int16, payload []byte) {
	var oldLen int
	if c.subChunks == nil {
		c.subChunks = make(map[int16]cachedBlockEntityPayload, 4)
	} else if prev, ok := c.subChunks[ind]; ok && prev.ready {
		oldLen = len(prev.payload)
	}
	c.subChunks[ind] = cachedBlockEntityPayload{payload: payload, ready: true}
	if c.col != nil {
		c.col.recordWireDelta(int64(len(payload)) - int64(oldLen))
	}
}

// CachedFullChunkPayload returns the cached full chunk raw payload.
func (c *ColumnCache) CachedFullChunkPayload() ([]byte, bool) {
	if c.full.ready {
		if c.col != nil {
			c.col.touchWireCache()
		}
		return c.full.payload, true
	}
	return c.full.payload, false
}

// CacheFullChunkPayload stores the full chunk raw payload.
func (c *ColumnCache) CacheFullChunkPayload(payload []byte) {
	var oldLen int
	if c.full.ready {
		oldLen = len(c.full.payload)
	}
	c.full = cachedBlockEntityPayload{payload: payload, ready: true}
	if c.col != nil {
		c.col.recordWireDelta(int64(len(payload)) - int64(oldLen))
	}
}

// invalidate clears the payload of the chunk, and those of the sub-chunks
// if subChunks is set, returning the total payload bytes freed.
func (c *ColumnCache) invalidate(subChunks bool) int64 {
	var freed int64
	if subChunks && len(c.subChunks) > 0 {
		for _, p := range c.subChunks {
			if p.ready {
				freed += int64(len(p.payload))
			}
		}
		clear(c.subChunks)
	}
	if c.full.ready {
		freed += int64(len(c.full.payload))
		c.full = cachedBlockEntityPayload{}
	}
	return freed
}

// clearAll releases the sub-chunk map and full payload on eviction or unload.
func (c *ColumnCache) clearAll() {
	c.subChunks = nil
	c.full = cachedBlockEntityPayload{}
}
