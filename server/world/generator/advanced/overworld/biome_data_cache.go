package overworld

import (
	"container/list"
	"sync"

	"github.com/df-mc/dragonfly/server/world"
)

type biomeData struct {
	genIDs   [10 * 10]int
	biomeIDs [16 * 16]int
}

type biomeDataCache struct {
	mu    sync.Mutex
	cap   int
	order *list.List
	index map[world.ChunkPos]*list.Element
}

type biomeDataCacheEntry struct {
	pos  world.ChunkPos
	data *biomeData
}

func newBiomeDataCache(capacity int) *biomeDataCache {
	if capacity < 1 {
		capacity = 1
	}
	return &biomeDataCache{
		cap:   capacity,
		order: list.New(),
		index: make(map[world.ChunkPos]*list.Element, capacity),
	}
}

func (c *biomeDataCache) get(pos world.ChunkPos) (*biomeData, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.index[pos]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(elem)
	return elem.Value.(*biomeDataCacheEntry).data, true
}

func (c *biomeDataCache) add(pos world.ChunkPos, data *biomeData) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.index[pos]; ok {
		c.order.MoveToFront(elem)
		elem.Value.(*biomeDataCacheEntry).data = data
		return
	}

	elem := c.order.PushFront(&biomeDataCacheEntry{pos: pos, data: data})
	c.index[pos] = elem
	if c.order.Len() <= c.cap {
		return
	}

	tail := c.order.Back()
	if tail == nil {
		return
	}
	c.order.Remove(tail)
	delete(c.index, tail.Value.(*biomeDataCacheEntry).pos)
}
