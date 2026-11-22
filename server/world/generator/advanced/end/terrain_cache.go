package end

import (
	"math/bits"
	"sync"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

const (
	endCacheShards        = 16
	endTerrainShardCap    = 64 // 1024 cached chunks (~4.5 MB max)
	endIslandTileShardCap = 64 // 1024 cached 16x16 island tiles (~64 KB max)
)

// endTerrainChunk stores the immutable raw End terrain for a single chunk.
// Raw End terrain only places EndStone in relative Y [0..127], so a 4 KB bitset
// represents the full chunk without allocating a chunk.Chunk.
type endTerrainChunk struct {
	bits            [512]uint64
	nonEmptySubMask uint8
	sourceBiome     endGenerationBiomes
}

func (t *endTerrainChunk) hasBlock(localX, relY, localZ int) bool {
	if uint(relY) >= 128 {
		return false
	}
	word := t.bits[((localX<<4)|localZ)<<1|(relY>>6)]
	return (word>>(uint(relY)&63))&1 != 0
}

// highestY returns the highest relative Y [0..127] containing EndStone in the
// column, or -1 if the column is empty.
func (t *endTerrainChunk) highestY(localX, localZ int) int {
	col := (localX << 4) | localZ
	if hi := t.bits[col<<1|1]; hi != 0 {
		return 127 - bits.LeadingZeros64(hi)
	}
	if lo := t.bits[col<<1]; lo != 0 {
		return 63 - bits.LeadingZeros64(lo)
	}
	return -1
}

// writeToChunk copies the raw EndStone blocks into c starting at c.Range().Min().
func (t *endTerrainChunk) writeToChunk(c *chunk.Chunk, endStoneRID uint32) {
	if t.nonEmptySubMask == 0 {
		return
	}
	baseY := int16(c.Range().Min())
	invalidatedHeightMap := false
	for subY := 0; subY < 8; subY++ {
		if t.nonEmptySubMask&(1<<uint(subY)) == 0 {
			continue
		}
		wordOffset := subY >> 2
		shift := uint((subY & 3) * 16)
		subBaseY := baseY + int16(subY*16)
		storage, _, _ := c.LayerStorageWithTwoRuntimeIDs(subBaseY, 0, endStoneRID, endStoneRID)
		for x := uint8(0); x < 16; x++ {
			colBase := int(x) << 4
			for z := uint8(0); z < 16; z++ {
				mask := uint16(t.bits[(colBase|int(z))<<1|wordOffset] >> shift)
				for mask != 0 {
					ly := uint8(bits.TrailingZeros16(mask))
					mask &= mask - 1
					if !invalidatedHeightMap {
						c.SetBlock(x, subBaseY+int16(ly), z, 0, endStoneRID)
						invalidatedHeightMap = true
						continue
					}
					storage.Set(x, ly, z, endStoneRID)
				}
			}
		}
	}
}

type endTerrainShard struct {
	mu      sync.RWMutex
	entries map[world.ChunkPos]*endTerrainChunk
}

type endTerrainCache struct {
	shards [endCacheShards]endTerrainShard
}

func endShardIndex(x, z int32) uint32 {
	h := uint32(x)*0x9e3779b1 ^ uint32(z)*0x85ebca77
	return (h ^ (h >> 16)) & (endCacheShards - 1)
}

func (c *endTerrainCache) get(pos world.ChunkPos) *endTerrainChunk {
	shard := &c.shards[endShardIndex(pos[0], pos[1])]
	shard.mu.RLock()
	t := shard.entries[pos]
	shard.mu.RUnlock()
	return t
}

func (c *endTerrainCache) put(pos world.ChunkPos, t *endTerrainChunk) *endTerrainChunk {
	shard := &c.shards[endShardIndex(pos[0], pos[1])]
	shard.mu.Lock()
	if shard.entries == nil {
		shard.entries = make(map[world.ChunkPos]*endTerrainChunk, endTerrainShardCap)
	}
	if existing := shard.entries[pos]; existing != nil {
		shard.mu.Unlock()
		return existing
	}
	if len(shard.entries) >= endTerrainShardCap {
		for k := range shard.entries {
			delete(shard.entries, k)
			break
		}
	}
	shard.entries[pos] = t
	shard.mu.Unlock()
	return t
}

// endIslandTile stores a 16x16 bitmask of outer-island centres where
// islandX^2 + islandZ^2 > 4096 and islandNoise.Sample2D < float32(-0.9).
type endIslandTile [16]uint16

type endIslandTileShard struct {
	mu      sync.RWMutex
	entries map[[2]int32]endIslandTile
}

type endIslandTileCache struct {
	shards [endCacheShards]endIslandTileShard
}

func (c *endIslandTileCache) get(key [2]int32) (endIslandTile, bool) {
	shard := &c.shards[endShardIndex(key[0], key[1])]
	shard.mu.RLock()
	tile, ok := shard.entries[key]
	shard.mu.RUnlock()
	return tile, ok
}

func (c *endIslandTileCache) put(key [2]int32, tile endIslandTile) {
	shard := &c.shards[endShardIndex(key[0], key[1])]
	shard.mu.Lock()
	if shard.entries == nil {
		shard.entries = make(map[[2]int32]endIslandTile, endIslandTileShardCap)
	}
	if len(shard.entries) >= endIslandTileShardCap {
		for k := range shard.entries {
			delete(shard.entries, k)
			break
		}
	}
	shard.entries[key] = tile
	shard.mu.Unlock()
}

type endCityStartShard struct {
	mu      sync.RWMutex
	entries map[world.ChunkPos]*endCityStructure
}

type endCityStartCache struct {
	shards [endCacheShards]endCityStartShard
}

func (c *endCityStartCache) get(pos world.ChunkPos) (*endCityStructure, bool) {
	shard := &c.shards[endShardIndex(pos[0], pos[1])]
	shard.mu.RLock()
	s, ok := shard.entries[pos]
	shard.mu.RUnlock()
	return s, ok
}

func (c *endCityStartCache) put(pos world.ChunkPos, s *endCityStructure) {
	shard := &c.shards[endShardIndex(pos[0], pos[1])]
	shard.mu.Lock()
	if shard.entries == nil {
		shard.entries = make(map[world.ChunkPos]*endCityStructure, endTerrainShardCap)
	}
	if len(shard.entries) >= endTerrainShardCap {
		for k := range shard.entries {
			delete(shard.entries, k)
			break
		}
	}
	shard.entries[pos] = s
	shard.mu.Unlock()
}
