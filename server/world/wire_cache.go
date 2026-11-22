package world

import "sync"

// defaultWireCacheMaxBytes caps the total encoded network payload bytes
// retained across all loaded columns in a World. 64 MiB holds roughly
// 1,500–2,000 hot full-detail town columns (~32–45 KiB each) while bounding
// transient exploration columns instead of retaining them until unload.
const defaultWireCacheMaxBytes int64 = 64 << 20

// wireCache tracks loaded columns holding cached encoded network payloads in
// an intrusive doubly-linked LRU list bounded by a byte budget. Its zero value
// is ready to use with defaultWireCacheMaxBytes.
//
// Columns cache and read wire payloads on the world owner, while block edits
// during a parallel region phase may invalidate disjoint columns concurrently;
// mu serialises all LRU list and byte-accounting mutations.
type wireCache struct {
	mu       sync.Mutex
	maxBytes int64
	bytes    int64
	count    int
	head     *Column // most recently used
	tail     *Column // least recently used
}

// wireCacheNode holds the intrusive LRU linkage and accounted payload bytes
// for a single Column. All fields except cache are guarded by cache.mu once
// the column is attached to a wireCache.
type wireCacheNode struct {
	cache  *wireCache
	prev   *Column
	next   *Column
	bytes  int64
	linked bool
}

// limit returns the effective byte budget of the cache.
func (wc *wireCache) limit() int64 {
	if wc.maxBytes > 0 {
		return wc.maxBytes
	}
	return defaultWireCacheMaxBytes
}

// setMaxBytes overrides the byte budget and evicts least-recently-used columns
// if the cache currently exceeds the new limit. A non-positive max restores
// defaultWireCacheMaxBytes.
func (wc *wireCache) setMaxBytes(max int64) {
	wc.mu.Lock()
	wc.maxBytes = max
	if wc.bytes > wc.limit() {
		wc.evictLocked(nil)
	}
	wc.mu.Unlock()
}

// stats returns the current tracked wire payload bytes and linked column count.
func (wc *wireCache) stats() (bytes int64, count int) {
	wc.mu.Lock()
	bytes, count = wc.bytes, wc.count
	wc.mu.Unlock()
	return bytes, count
}

// touch promotes c to the most-recently-used front of the list if it is linked.
func (wc *wireCache) touch(c *Column) {
	wc.mu.Lock()
	if c.wire.linked && wc.head != c {
		wc.moveToFrontLocked(c)
	}
	wc.mu.Unlock()
}

// recordDelta updates c's tracked byte size by delta, promotes c to the MRU
// front (or unlinks it if its bytes drop to zero), and evicts LRU tail columns
// until total bytes fit within the budget.
func (wc *wireCache) recordDelta(c *Column, delta int64) {
	wc.mu.Lock()
	oldBytes := c.wire.bytes
	newBytes := oldBytes + delta
	if newBytes < 0 {
		newBytes = 0
	}
	c.wire.bytes = newBytes
	if c.wire.linked {
		wc.bytes += newBytes - oldBytes
		if newBytes == 0 {
			wc.unlinkLocked(c)
		} else if wc.head != c {
			wc.moveToFrontLocked(c)
		}
	} else if newBytes > 0 {
		wc.pushFrontLocked(c)
		wc.bytes += newBytes
	}
	if delta > 0 && wc.bytes > wc.limit() {
		wc.evictLocked(c)
	}
	wc.mu.Unlock()
}

// subBytes subtracts freed bytes from c without promoting it, unlinking c if
// no cached payload bytes remain.
func (wc *wireCache) subBytes(c *Column, freed int64) {
	wc.mu.Lock()
	if c.wire.linked {
		if freed >= c.wire.bytes {
			wc.bytes -= c.wire.bytes
			c.wire.bytes = 0
			wc.unlinkLocked(c)
		} else {
			c.wire.bytes -= freed
			wc.bytes -= freed
		}
	} else {
		c.wire.bytes -= freed
		if c.wire.bytes < 0 {
			c.wire.bytes = 0
		}
	}
	wc.mu.Unlock()
}

// adopt links a newly attached column that already holds cached payload bytes
// and enforces the byte budget.
func (wc *wireCache) adopt(c *Column) {
	wc.mu.Lock()
	if c.wire.bytes > 0 && !c.wire.linked {
		wc.pushFrontLocked(c)
		wc.bytes += c.wire.bytes
		if wc.bytes > wc.limit() {
			wc.evictLocked(c)
		}
	}
	wc.mu.Unlock()
}

// detach removes c from wc while leaving c's own payload byte count intact so
// it can be attached to another cache.
func (wc *wireCache) detach(c *Column) {
	wc.mu.Lock()
	if c.wire.linked {
		wc.unlinkLocked(c)
		wc.bytes -= c.wire.bytes
	}
	c.wire.cache = nil
	wc.mu.Unlock()
}

// release unlinks c from wc, subtracts its bytes, and clears its cache pointer.
func (wc *wireCache) release(c *Column) {
	wc.mu.Lock()
	if c.wire.linked {
		wc.unlinkLocked(c)
		wc.bytes -= c.wire.bytes
	}
	c.wire.bytes = 0
	c.wire.cache = nil
	wc.mu.Unlock()
}

// evictLocked evicts least-recently-used columns from the tail until total
// bytes are within the budget, never self-evicting keep so an in-progress
// encode on keep remains cached.
func (wc *wireCache) evictLocked(keep *Column) {
	limit := wc.limit()
	for wc.bytes > limit && wc.tail != nil && wc.tail != keep {
		victim := wc.tail
		wc.unlinkLocked(victim)
		wc.bytes -= victim.wire.bytes
		victim.wire.bytes = 0
		victim.clearAllWirePayloads()
	}
}

// pushFrontLocked inserts an unlinked column at the MRU head of the list.
func (wc *wireCache) pushFrontLocked(c *Column) {
	c.wire.prev = nil
	c.wire.next = wc.head
	if wc.head != nil {
		wc.head.wire.prev = c
	} else {
		wc.tail = c
	}
	wc.head = c
	c.wire.linked = true
	wc.count++
}

// unlinkLocked removes a linked column from the list.
func (wc *wireCache) unlinkLocked(c *Column) {
	if c.wire.prev != nil {
		c.wire.prev.wire.next = c.wire.next
	} else {
		wc.head = c.wire.next
	}
	if c.wire.next != nil {
		c.wire.next.wire.prev = c.wire.prev
	} else {
		wc.tail = c.wire.prev
	}
	c.wire.prev = nil
	c.wire.next = nil
	c.wire.linked = false
	wc.count--
}

// moveToFrontLocked moves an already-linked non-head column to the MRU head.
func (wc *wireCache) moveToFrontLocked(c *Column) {
	c.wire.prev.wire.next = c.wire.next
	if c.wire.next != nil {
		c.wire.next.wire.prev = c.wire.prev
	} else {
		wc.tail = c.wire.prev
	}
	c.wire.prev = nil
	c.wire.next = wc.head
	wc.head.wire.prev = c
	wc.head = c
}

// attachWireCache binds c to wc, adopting any already-cached wire payloads.
func (c *Column) attachWireCache(wc *wireCache) {
	if c.ownCache.col == nil {
		c.ownCache.col = c
	}
	if c.wire.cache == wc {
		return
	}
	if prev := c.wire.cache; prev != nil {
		prev.detach(c)
	}
	c.wire.cache = wc
	if wc != nil && c.wire.bytes > 0 {
		wc.adopt(c)
	}
}

// releaseWireCache unlinks c from its world's wire cache and drops all cached
// encoded network payloads so unloaded columns retain no pointers or buffers.
func (c *Column) releaseWireCache() {
	if wc := c.wire.cache; wc != nil {
		wc.release(c)
	} else {
		c.wire.bytes = 0
	}
	c.clearAllWirePayloads()
}

// touchWireCache promotes c in its world's wire cache on a cache hit.
func (c *Column) touchWireCache() {
	if wc := c.wire.cache; wc != nil {
		wc.touch(c)
	}
}

// recordWireDelta adjusts c's tracked wire payload bytes after a cache write.
func (c *Column) recordWireDelta(delta int64) {
	if wc := c.wire.cache; wc != nil {
		wc.recordDelta(c, delta)
		return
	}
	c.wire.bytes += delta
	if c.wire.bytes < 0 {
		c.wire.bytes = 0
	}
}

// subWireBytes subtracts freed bytes after a payload invalidation.
func (c *Column) subWireBytes(freed int64) {
	if freed <= 0 {
		return
	}
	if wc := c.wire.cache; wc != nil {
		wc.subBytes(c, freed)
		return
	}
	c.wire.bytes -= freed
	if c.wire.bytes < 0 {
		c.wire.bytes = 0
	}
}

// clearAllWirePayloads releases all cached wire payload buffers and maps on c.
// Keyed ColumnCache handles in c.caches are kept so existing handles remain
// valid, while their sub-chunk maps and full payloads are cleared.
func (c *Column) clearAllWirePayloads() {
	c.subChunkHeightMaps = nil
	c.networkBiomePayload = cachedBlockEntityPayload{}
	c.limitedChunkPayload = cachedBlockEntityPayload{}
	c.chunkBlockEntityPayload = cachedBlockEntityPayload{}
	c.chunkBlockEntityPayloadNoBorder = cachedBlockEntityPayload{}
	c.subChunkBlockEntityPayloads = nil
	c.ownCache.clearAll()
	for _, cache := range c.caches {
		cache.clearAll()
	}
}
