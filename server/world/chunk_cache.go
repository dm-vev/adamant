package world

// chunkCacheSlot caches one chunk position and its settled Column.
type chunkCacheSlot struct {
	pos ChunkPos
	col *Column
}

// chunkCache is a 4-slot associative ring cache with a 1-slot MRU fast hit
// checked inline before map lookup on Tx, World, and ReadView.
type chunkCache struct {
	last  chunkCacheSlot
	slots [4]chunkCacheSlot
	next  uint8
}

func (c *chunkCache) get(pos ChunkPos) *Column {
	if c.last.col != nil && c.last.pos == pos {
		return c.last.col
	}
	s := &c.slots
	if s[0].col != nil && s[0].pos == pos {
		c.last = s[0]
		return s[0].col
	}
	if s[1].col != nil && s[1].pos == pos {
		c.last = s[1]
		return s[1].col
	}
	if s[2].col != nil && s[2].pos == pos {
		c.last = s[2]
		return s[2].col
	}
	if s[3].col != nil && s[3].pos == pos {
		c.last = s[3]
		return s[3].col
	}
	return nil
}

func (c *chunkCache) put(pos ChunkPos, col *Column) {
	c.last = chunkCacheSlot{pos: pos, col: col}
	s := &c.slots
	if s[0].col != nil && s[0].pos == pos {
		s[0].col = col
		return
	}
	if s[1].col != nil && s[1].pos == pos {
		s[1].col = col
		return
	}
	if s[2].col != nil && s[2].pos == pos {
		s[2].col = col
		return
	}
	if s[3].col != nil && s[3].pos == pos {
		s[3].col = col
		return
	}
	s[c.next] = chunkCacheSlot{pos: pos, col: col}
	c.next = (c.next + 1) & 3
}

func (c *chunkCache) invalidate(pos ChunkPos) {
	if c.last.col != nil && c.last.pos == pos {
		c.last = chunkCacheSlot{}
	}
	for i := range c.slots {
		if c.slots[i].col != nil && c.slots[i].pos == pos {
			c.slots[i] = chunkCacheSlot{}
		}
	}
}

func (c *chunkCache) clear() {
	*c = chunkCache{}
}
