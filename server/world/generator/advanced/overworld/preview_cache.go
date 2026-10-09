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
	mu      sync.Mutex
	cap     int
	order   *list.List
	index   map[world.ChunkPos]*list.Element
	flights map[world.ChunkPos]*previewFlight
}

type previewCacheEntry struct {
	pos            world.ChunkPos
	c              *chunk.Chunk
	postProcessing [][]uint16
	structures     *modernStructureContext
}

type previewFlight struct {
	done    chan struct{}
	entry   *previewCacheEntry
	failure any
}

func newPreviewCache(capacity int) *previewCache {
	if capacity < 1 {
		capacity = 1
	}
	return &previewCache{
		cap:     capacity,
		order:   list.New(),
		index:   make(map[world.ChunkPos]*list.Element, capacity),
		flights: make(map[world.ChunkPos]*previewFlight),
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

// load coalesces concurrent requests without holding the LRU lock while building.
// Raw terrain and surfaced terrain use separate caches, so their dependencies
// never wait on the same in-flight entry.
func (c *previewCache) load(pos world.ChunkPos, build func() *previewCacheEntry) *previewCacheEntry {
	c.mu.Lock()
	if elem, ok := c.index[pos]; ok {
		c.order.MoveToFront(elem)
		entry := elem.Value.(*previewCacheEntry)
		c.mu.Unlock()
		return entry
	}
	if flight, ok := c.flights[pos]; ok {
		c.mu.Unlock()
		<-flight.done
		if flight.failure != nil {
			panic(flight.failure)
		}
		return flight.entry
	}
	flight := &previewFlight{done: make(chan struct{})}
	c.flights[pos] = flight
	c.mu.Unlock()

	func() {
		defer func() { flight.failure = recover() }()
		flight.entry = build()
	}()

	c.mu.Lock()
	delete(c.flights, pos)
	if flight.failure == nil {
		c.addLocked(flight.entry)
	}
	close(flight.done)
	c.mu.Unlock()
	if flight.failure != nil {
		panic(flight.failure)
	}
	return flight.entry
}

func (c *previewCache) add(pos world.ChunkPos, ch *chunk.Chunk) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.addLocked(&previewCacheEntry{pos: pos, c: ch})
}

func (c *previewCache) addLocked(entry *previewCacheEntry) {
	pos := entry.pos
	if elem, ok := c.index[pos]; ok {
		c.order.MoveToFront(elem)
		elem.Value = entry
		return
	}

	elem := c.order.PushFront(entry)
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
