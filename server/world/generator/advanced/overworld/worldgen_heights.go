package overworld

import (
	"sync"

	"github.com/df-mc/dragonfly/server/world"
)

// worldgenHeights holds the WORLDGEN heightmaps of the base terrain of a
// chunk, WORLD_SURFACE_WG and OCEAN_FLOOR_WG, for each of its columns. They
// are frozen at the base terrain, so they never change once found: a feature
// whose box reaches into a chunk whose base terrain left the preview cache
// reads them here instead of building that terrain again for one height.
type worldgenHeights struct {
	// minY and maxY are the build heights the heights were found within.
	minY, maxY int32
	// height holds the heights by worldgenHeightIndex, then column.
	height [2][256]int16
}

// worldgenHeightIndex returns the index in worldgenHeights.height of the
// frozen heightmap kind.
func worldgenHeightIndex(kind uint8) int { return int(kind >> 1) }

// worldgenHeightCacheSize bounds the chunks whose WORLDGEN heightmaps are
// kept, at 1 KiB a chunk: several times the chunks the preview cache holds,
// so that the frontiers of many players generating at once fit.
const worldgenHeightCacheSize = 16384

// worldgenHeightShards splits the cache so that the workers reading it do not
// wait on one lock.
const worldgenHeightShards = 32

// worldgenHeightCache keeps the WORLDGEN heightmaps of the base terrain of
// recently generated chunks. Each shard forgets its oldest chunk first.
type worldgenHeightCache struct {
	shards [worldgenHeightShards]worldgenHeightShard
}

type worldgenHeightShard struct {
	mu      sync.Mutex
	entries map[world.ChunkPos]*worldgenHeights
	// order holds the positions of entries in the order they were added,
	// as a ring: next is the oldest once the ring is full.
	order []world.ChunkPos
	next  int
}

func (c *worldgenHeightCache) shard(pos world.ChunkPos) *worldgenHeightShard {
	h := uint32(pos[0])*0x9e3779b1 ^ uint32(pos[1])*0x85ebca77
	return &c.shards[(h^h>>15)%worldgenHeightShards]
}

// get returns the heights of the chunk at pos, or nil if they are not kept.
func (c *worldgenHeightCache) get(pos world.ChunkPos) *worldgenHeights {
	s := c.shard(pos)
	s.mu.Lock()
	h := s.entries[pos]
	s.mu.Unlock()
	return h
}

// put keeps the heights of the chunk at pos, forgetting the oldest chunk of
// its shard if the shard is full.
func (c *worldgenHeightCache) put(pos world.ChunkPos, h *worldgenHeights) {
	const perShard = worldgenHeightCacheSize / worldgenHeightShards
	s := c.shard(pos)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries == nil {
		s.entries = make(map[world.ChunkPos]*worldgenHeights, perShard)
	}
	if _, ok := s.entries[pos]; ok {
		s.entries[pos] = h
		return
	}
	if len(s.order) < perShard {
		s.order = append(s.order, pos)
	} else {
		delete(s.entries, s.order[s.next])
		s.order[s.next] = pos
		s.next = (s.next + 1) % perShard
	}
	s.entries[pos] = h
}

// clear forgets the heights of every chunk.
func (c *worldgenHeightCache) clear() {
	for i := range c.shards {
		s := &c.shards[i]
		s.mu.Lock()
		s.entries, s.order, s.next = nil, nil, 0
		s.mu.Unlock()
	}
}

// worldgenHeight returns the height of the frozen heightmap kind in the
// column at x, z: the one placementHeight would find scanning the base
// terrain, read from the heights of its chunk, which are found for every
// column of the chunk once and kept for the generator.
func (r *featureRegion) worldgenHeight(x, z int32, kind uint8) int32 {
	pos := world.ChunkPos{x >> 4, z >> 4}
	h := r.lastHeights
	if h == nil || r.lastHeightsPos != pos {
		cache := &r.base.generator.worldgenHeights
		if h = cache.get(pos); h == nil || h.minY != r.MinY() || h.maxY != r.MaxY() {
			h = r.findWorldgenHeights(pos)
			cache.put(pos, h)
		}
		r.lastHeightsPos, r.lastHeights = pos, h
	}
	return int32(h.height[worldgenHeightIndex(kind)][(x&15)|(z&15)<<4])
}

// findWorldgenHeights scans every column of the base terrain of the chunk at
// pos for the WORLDGEN heightmaps, as placementHeight scans one.
func (r *featureRegion) findWorldgenHeights(pos world.ChunkPos) *worldgenHeights {
	_, surfaceMask, _ := heightmapOf("WORLD_SURFACE_WG")
	floorKind, floorMask, _ := heightmapOf("OCEAN_FLOOR_WG")
	surfaceKind, _, _ := heightmapOf("WORLD_SURFACE_WG")
	h := &worldgenHeights{minY: r.MinY(), maxY: r.MaxY()}
	base := r.baseChunk(pos)
	for lz := range int32(16) {
		for lx := range int32(16) {
			x, z := pos[0]<<4+lx, pos[1]<<4+lz
			surface, floor := r.MinY(), r.MinY()
			foundSurface, foundFloor := false, false
			top := min(r.MaxY(), NativeColumnTop(base, uint8(lx), uint8(lz)))
			for y := top; y >= r.MinY() && (!foundSurface || !foundFloor); y-- {
				flags := r.f.heightmapFlags(r.c, r.baseState(featurePos{x, y, z}))
				if !foundSurface && flags&surfaceMask != 0 {
					surface, foundSurface = y+1, true
				}
				if !foundFloor && flags&floorMask != 0 {
					floor, foundFloor = y+1, true
				}
			}
			column := lx | lz<<4
			h.height[worldgenHeightIndex(surfaceKind)][column] = int16(surface)
			h.height[worldgenHeightIndex(floorKind)][column] = int16(floor)
		}
	}
	return h
}
