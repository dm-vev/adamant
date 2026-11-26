package overworld

import (
	"sync"

	"github.com/dm-vev/adamant/server/world"
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
	mu      sync.RWMutex
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
	s.mu.RLock()
	h := s.entries[pos]
	s.mu.RUnlock()
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

// cachedWorldgenHeights returns the WORLDGEN heightmaps for pos if already
// computed and cached, without triggering terrain generation.
func (r *featureRegion) cachedWorldgenHeights(pos world.ChunkPos) *worldgenHeights {
	if h := r.lastHeights; h != nil && r.lastHeightsPos == pos {
		return h
	}
	if r.baseGridValid {
		gx, gz := uint32(pos[0]-r.baseGridOriginX), uint32(pos[1]-r.baseGridOriginZ)
		if gx < 5 && gz < 5 {
			idx := gx*5 + gz
			if h := r.heightsGrid[idx]; h != nil {
				r.lastHeightsPos, r.lastHeights = pos, h
				return h
			}
			if h := r.base.generator.worldgenHeights.get(pos); h != nil && h.minY == r.minY && h.maxY == r.maxY {
				r.heightsGrid[idx] = h
				r.lastHeightsPos, r.lastHeights = pos, h
				return h
			}
			return nil
		}
	}
	if h := r.base.generator.worldgenHeights.get(pos); h != nil && h.minY == r.minY && h.maxY == r.maxY {
		r.lastHeightsPos, r.lastHeights = pos, h
		return h
	}
	return nil
}

// worldgenHeight returns the height of the frozen heightmap kind in the
// column at x, z: the one placementHeight would find scanning the base
// terrain, read from the heights of its chunk, which are found for every
// column of the chunk once and kept for the generator.
func (r *featureRegion) worldgenHeight(x, z int32, kind uint8) int32 {
	pos := world.ChunkPos{x >> 4, z >> 4}
	h := r.lastHeights
	if h == nil || r.lastHeightsPos != pos {
		var gridIdx uint32
		inGrid := false
		if r.baseGridValid {
			gx, gz := uint32(pos[0]-r.baseGridOriginX), uint32(pos[1]-r.baseGridOriginZ)
			if gx < 5 && gz < 5 {
				gridIdx, inGrid = gx*5+gz, true
				h = r.heightsGrid[gridIdx]
			}
		}
		if h == nil {
			cache := &r.base.generator.worldgenHeights
			if h = cache.get(pos); h == nil || h.minY != r.minY || h.maxY != r.maxY {
				h = r.findWorldgenHeights(pos)
				cache.put(pos, h)
			}
			if inGrid {
				r.heightsGrid[gridIdx] = h
			}
		}
		r.lastHeightsPos, r.lastHeights = pos, h
	}
	return int32(h.height[worldgenHeightIndex(kind)][(x&15)|(z&15)<<4])
}

// findWorldgenHeights scans every column of the base terrain of the chunk at
// pos for the WORLDGEN heightmaps, as placementHeight scans one.
func (r *featureRegion) findWorldgenHeights(pos world.ChunkPos) *worldgenHeights {
	surfaceKind, surfaceMask, _ := heightmapOf("WORLD_SURFACE_WG")
	floorKind, floorMask, _ := heightmapOf("OCEAN_FLOOR_WG")
	minY, maxY := r.minY, r.maxY
	h := &worldgenHeights{minY: minY, maxY: maxY}
	base := r.baseChunk(pos)
	subs := base.Sub()
	baseRange := base.Range()
	baseMinY, baseMaxY := int32(baseRange[0]), int32(baseRange[1])
	topCap := min(maxY, baseMaxY)
	for i := len(subs) - 1; i >= 0; i-- {
		if !subs[i].Empty() {
			subTop := int32(base.SubY(int16(i))) + 15
			if subTop < topCap {
				topCap = subTop
			}
			break
		}
		if i == 0 {
			for col := range 256 {
				h.height[worldgenHeightIndex(surfaceKind)][col] = int16(minY)
				h.height[worldgenHeightIndex(floorKind)][col] = int16(minY)
			}
			return h
		}
	}
	airRID := r.airRID
	isDefault := r.isDefaultReg
	surfaceIdx := worldgenHeightIndex(surfaceKind)
	floorIdx := worldgenHeightIndex(floorKind)
	for lz := range int32(16) {
		for lx := range int32(16) {
			surface, floor := minY, minY
			foundSurface, foundFloor := false, false
			for y := topCap; y >= minY && (!foundSurface || !foundFloor); y-- {
				if y < baseMinY || y > baseMaxY {
					continue
				}
				subIdx := int(y-baseMinY) >> 4
				if subs[subIdx].Empty() {
					y = baseMinY + int32(subIdx)<<4
					continue
				}
				blk, liq, stateID := base.Native(uint8(lx), int16(y), uint8(lz))
				if blk == airRID && liq == airRID && stateID == 0 {
					continue
				}
				var isSurface, isFloor bool
				if isDefault && liq == airRID && stateID == 0 {
					meta := featureMetaForDefaultRID(blk)
					if meta&featFlagCanonicalErr == 0 {
						isSurface = meta&featFlagWorldSurface != 0
						isFloor = meta&featFlagMotion != 0
					} else {
						flags := r.f.heightmapFlags(r.c, world.NativeState{Block: blk, Liquid: liq, StateID: stateID})
						isSurface = flags&surfaceMask != 0
						isFloor = flags&floorMask != 0
					}
				} else {
					flags := r.f.heightmapFlags(r.c, world.NativeState{Block: blk, Liquid: liq, StateID: stateID})
					isSurface = flags&surfaceMask != 0
					isFloor = flags&floorMask != 0
				}
				if !foundSurface && isSurface {
					surface, foundSurface = y+1, true
				}
				if !foundFloor && isFloor {
					floor, foundFloor = y+1, true
				}
			}
			column := lx | lz<<4
			h.height[surfaceIdx][column] = int16(surface)
			h.height[floorIdx][column] = int16(floor)
		}
	}
	return h
}
