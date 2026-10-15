package overworld

import (
	"container/list"
	"sync"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// previewCacheShards splits the cache so generation workers reading terrain
// previews of different chunks do not wait on one lock.
const previewCacheShards = 32

// previewCache stores recently generated terrain previews used by decoration.
// Entries are immutable and safe to share between chunk generations.
type previewCache struct {
	shards [previewCacheShards]previewCacheShard
}

// previewCacheShard is a least-recently-used cache of the previews of the
// chunks hashing to it.
type previewCacheShard struct {
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

// newPreviewCache returns a cache holding about capacity previews in total.
func newPreviewCache(capacity int) *previewCache {
	perShard := max((capacity+previewCacheShards-1)/previewCacheShards, 1)
	c := &previewCache{}
	for i := range c.shards {
		c.shards[i] = previewCacheShard{
			cap:     perShard,
			order:   list.New(),
			index:   make(map[world.ChunkPos]*list.Element, perShard),
			flights: make(map[world.ChunkPos]*previewFlight),
		}
	}
	return c
}

func (c *previewCache) shard(pos world.ChunkPos) *previewCacheShard {
	h := uint32(pos[0])*0x9e3779b1 ^ uint32(pos[1])*0x85ebca77
	return &c.shards[(h^h>>15)%previewCacheShards]
}

func (c *previewCache) get(pos world.ChunkPos) (*chunk.Chunk, bool) {
	s := c.shard(pos)
	s.mu.Lock()
	defer s.mu.Unlock()

	elem, ok := s.index[pos]
	if !ok {
		return nil, false
	}
	s.order.MoveToFront(elem)
	return elem.Value.(*previewCacheEntry).c, true
}

// load coalesces concurrent requests without holding the LRU lock while building.
// Raw terrain and surfaced terrain use separate caches, so their dependencies
// never wait on the same in-flight entry.
func (c *previewCache) load(pos world.ChunkPos, build func() *previewCacheEntry) *previewCacheEntry {
	s := c.shard(pos)
	s.mu.Lock()
	if elem, ok := s.index[pos]; ok {
		s.order.MoveToFront(elem)
		entry := elem.Value.(*previewCacheEntry)
		s.mu.Unlock()
		return entry
	}
	if flight, ok := s.flights[pos]; ok {
		s.mu.Unlock()
		<-flight.done
		if flight.failure != nil {
			panic(flight.failure)
		}
		return flight.entry
	}
	flight := &previewFlight{done: make(chan struct{})}
	s.flights[pos] = flight
	s.mu.Unlock()

	func() {
		defer func() { flight.failure = recover() }()
		flight.entry = build()
	}()

	s.mu.Lock()
	delete(s.flights, pos)
	if flight.failure == nil {
		s.addLocked(flight.entry)
	}
	close(flight.done)
	s.mu.Unlock()
	if flight.failure != nil {
		panic(flight.failure)
	}
	return flight.entry
}

func (c *previewCache) add(pos world.ChunkPos, ch *chunk.Chunk) {
	s := c.shard(pos)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.addLocked(&previewCacheEntry{pos: pos, c: ch})
}

func (s *previewCacheShard) addLocked(entry *previewCacheEntry) {
	pos := entry.pos
	if elem, ok := s.index[pos]; ok {
		s.order.MoveToFront(elem)
		elem.Value = entry
		return
	}

	elem := s.order.PushFront(entry)
	s.index[pos] = elem
	if s.order.Len() <= s.cap {
		return
	}

	tail := s.order.Back()
	if tail == nil {
		return
	}
	s.order.Remove(tail)
	delete(s.index, tail.Value.(*previewCacheEntry).pos)
}
