package nether

import (
	"container/list"
	"sync"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

const netherTerrainCacheShards = 32

type netherTerrainCache struct {
	shards [netherTerrainCacheShards]netherTerrainCacheShard
}

type netherTerrainCacheShard struct {
	mu      sync.Mutex
	cap     int
	order   *list.List
	index   map[world.ChunkPos]*list.Element
	flights map[world.ChunkPos]*netherTerrainFlight
}

type netherTerrainEntry struct {
	pos world.ChunkPos
	c   *chunk.Chunk
}

type netherTerrainFlight struct {
	done    chan struct{}
	c       *chunk.Chunk
	failure any
}

func newNetherTerrainCache(capacity int) *netherTerrainCache {
	perShard := max((capacity+netherTerrainCacheShards-1)/netherTerrainCacheShards, 1)
	cache := &netherTerrainCache{}
	for i := range cache.shards {
		cache.shards[i] = netherTerrainCacheShard{
			cap:     perShard,
			order:   list.New(),
			index:   make(map[world.ChunkPos]*list.Element, perShard),
			flights: make(map[world.ChunkPos]*netherTerrainFlight),
		}
	}
	return cache
}

func (c *netherTerrainCache) shard(pos world.ChunkPos) *netherTerrainCacheShard {
	h := uint32(pos[0])*0x9e3779b1 ^ uint32(pos[1])*0x85ebca77
	return &c.shards[(h^h>>15)%netherTerrainCacheShards]
}

func (c *netherTerrainCache) load(pos world.ChunkPos, build func() *chunk.Chunk) *chunk.Chunk {
	s := c.shard(pos)
	s.mu.Lock()
	if elem, ok := s.index[pos]; ok {
		s.order.MoveToFront(elem)
		ch := elem.Value.(*netherTerrainEntry).c
		s.mu.Unlock()
		return ch
	}
	if flight, ok := s.flights[pos]; ok {
		s.mu.Unlock()
		<-flight.done
		if flight.failure != nil {
			panic(flight.failure)
		}
		return flight.c
	}
	flight := &netherTerrainFlight{done: make(chan struct{})}
	s.flights[pos] = flight
	s.mu.Unlock()

	func() {
		defer func() { flight.failure = recover() }()
		flight.c = build()
	}()

	s.mu.Lock()
	delete(s.flights, pos)
	if flight.failure == nil {
		elem := s.order.PushFront(&netherTerrainEntry{pos: pos, c: flight.c})
		s.index[pos] = elem
		if s.order.Len() > s.cap {
			tail := s.order.Back()
			s.order.Remove(tail)
			delete(s.index, tail.Value.(*netherTerrainEntry).pos)
		}
	}
	close(flight.done)
	s.mu.Unlock()
	if flight.failure != nil {
		panic(flight.failure)
	}
	return flight.c
}
