package world

// chunkCacheSlot caches one chunk position and its settled Column.
type chunkCacheSlot struct {
	pos ChunkPos
	col *Column
}

// chunkCache is a 4-slot associative ring cache checked inline before map
// lookup on Tx, World, and ReadView.
type chunkCache struct {
	slots [4]chunkCacheSlot
	next  uint8
}

func (c *chunkCache) get(pos ChunkPos) *Column {
	for i := range c.slots {
		if c.slots[i].col != nil && c.slots[i].pos == pos {
			return c.slots[i].col
		}
	}
	return nil
}

func (c *chunkCache) put(pos ChunkPos, col *Column) {
	for i := range c.slots {
		if c.slots[i].col != nil && c.slots[i].pos == pos {
			c.slots[i].col = col
			return
		}
	}
	c.slots[c.next] = chunkCacheSlot{pos: pos, col: col}
	c.next = (c.next + 1) & 3
}

func (c *chunkCache) invalidate(pos ChunkPos) {
	for i := range c.slots {
		if c.slots[i].col != nil && c.slots[i].pos == pos {
			c.slots[i] = chunkCacheSlot{}
		}
	}
}

func (c *chunkCache) clear() {
	*c = chunkCache{}
}
