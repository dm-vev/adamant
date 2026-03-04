package overworld

import (
	"container/list"
	"sync"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// previewCache stores recently generated terrain previews used by decoration.
// Entries are immutable and safe to share between chunk generations.
type previewCache struct {
	mu    sync.Mutex
	cap   int
	order *list.List
	index map[world.ChunkPos]*list.Element
}

type previewCacheEntry struct {
	pos world.ChunkPos
	c   *chunk.Chunk
}

func newPreviewCache(capacity int) *previewCache {
	if capacity < 1 {
		capacity = 1
	}
	return &previewCache{
		cap:   capacity,
		order: list.New(),
		index: make(map[world.ChunkPos]*list.Element, capacity),
	}
}

func (c *previewCache) get(pos world.ChunkPos) (*chunk.Chunk, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.index[pos]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(elem)
	return elem.Value.(*previewCacheEntry).c, true
}

func (c *previewCache) add(pos world.ChunkPos, ch *chunk.Chunk) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.index[pos]; ok {
		c.order.MoveToFront(elem)
		elem.Value.(*previewCacheEntry).c = ch
		return
	}

	elem := c.order.PushFront(&previewCacheEntry{pos: pos, c: ch})
	c.index[pos] = elem
	if c.order.Len() <= c.cap {
		return
	}

	tail := c.order.Back()
	if tail == nil {
		return
	}
	c.order.Remove(tail)
	delete(c.index, tail.Value.(*previewCacheEntry).pos)
}
