package overworld

import (
	"sync"

	"github.com/dm-vev/adamant/server/internal/vanilla"
	"github.com/dm-vev/adamant/server/world"
)

// Noise biomes are sampled at quart heights -16 to 79; NoiseBiomeAt
// clamps other heights to them.
const (
	chunkBiomeMinQuartY = -16
	chunkBiomeQuartsY   = 96
)

// chunkBiomes holds the Java noise biome index of every quart of a chunk,
// as Java keeps the biomes of a chunk in its sections once sampled.
type chunkBiomes struct {
	index [4][4][chunkBiomeQuartsY]uint8
}

// chunkBiomeShards and chunkBiomeShardSize bound the chunk biome cache.
const (
	chunkBiomeShards    = 32
	chunkBiomeShardSize = 1024
)

const (
	originBiomeShards = 32
	originBiomeSlots  = 512 // power of two; 32 * 512 = 16384 slots (~192 KiB total)
)

type originBiomeSlot struct {
	pos   world.ChunkPos
	biome uint8
	valid bool
}

// chunkBiomeCache keeps the noise biomes of recently decorated chunks: every
// chunk is read by the decoration of the nine chunks around it.
type chunkBiomeCache struct {
	shards [chunkBiomeShards]struct {
		mu     sync.RWMutex
		biomes map[world.ChunkPos]*chunkBiomes
	}
	origins [originBiomeShards]struct {
		mu    sync.RWMutex
		slots [originBiomeSlots]originBiomeSlot
	}
}

func chunkPosHash(pos world.ChunkPos) uint32 {
	h := uint32(pos[0])*0x9e3779b1 ^ uint32(pos[1])*0x85ebca77
	return h ^ (h >> 15)
}

// get returns the cached noise biomes of the chunk, if any.
func (c *chunkBiomeCache) get(pos world.ChunkPos) (*chunkBiomes, bool) {
	shard := &c.shards[chunkPosHash(pos)%chunkBiomeShards]
	shard.mu.RLock()
	biomes, ok := shard.biomes[pos]
	shard.mu.RUnlock()
	return biomes, ok
}

func (c *chunkBiomeCache) load(pos world.ChunkPos, compute func() *chunkBiomes) *chunkBiomes {
	if biomes, ok := c.get(pos); ok {
		return biomes
	}
	shard := &c.shards[chunkPosHash(pos)%chunkBiomeShards]
	biomes := compute()
	shard.mu.Lock()
	if shard.biomes == nil || len(shard.biomes) >= chunkBiomeShardSize {
		shard.biomes = make(map[world.ChunkPos]*chunkBiomes, chunkBiomeShardSize)
	}
	shard.biomes[pos] = biomes
	shard.mu.Unlock()
	return biomes
}

// originBiome returns the cached noise biome index at quart (pos[0]*4, 0, pos[1]*4),
// computing and caching it on miss. Used by the 17x17 carver neighborhood.
func (c *chunkBiomeCache) originBiome(pos world.ChunkPos, noise *vanilla.OverworldNoise) uint8 {
	if biomes, ok := c.get(pos); ok {
		return biomes.index[0][0][-chunkBiomeMinQuartY]
	}
	h := chunkPosHash(pos)
	shard := &c.origins[h%originBiomeShards]
	slotIdx := (h / originBiomeShards) & (originBiomeSlots - 1)
	shard.mu.RLock()
	slot := shard.slots[slotIdx]
	shard.mu.RUnlock()
	if slot.valid && slot.pos == pos {
		return slot.biome
	}
	var hint vanilla.ClimateHint
	biome := noise.SampleBiomeColumn(pos[0]*16, pos[1]*16).BiomeIndexNear(0, &hint)
	shard.mu.Lock()
	shard.slots[slotIdx] = originBiomeSlot{pos: pos, biome: biome, valid: true}
	shard.mu.Unlock()
	return biome
}

func fillNoiseBiomeColumn(column vanilla.OverworldColumn, dst *[chunkBiomeQuartsY]uint8, hint *vanilla.ClimateHint) {
	vanilla.FillOverworldBiomeColumn(column, dst, hint)
}

// noiseBiomes returns the noise biomes of the chunk, sampling each of its
// sixteen quart columns once.
func (g *Overworld) noiseBiomes(pos world.ChunkPos) *chunkBiomes {
	return g.chunkBiomes.load(pos, func() *chunkBiomes {
		biomes := &chunkBiomes{}
		var hint vanilla.ClimateHint
		for z := range int32(4) {
			for x := range int32(4) {
				column := g.noise.SampleBiomeColumn((pos[0]*4+x)*4, (pos[1]*4+z)*4)
				fillNoiseBiomeColumn(column, &biomes.index[x][z], &hint)
			}
		}
		return biomes
	})
}

func (g *Overworld) loadQuartBiomes(pos world.ChunkPos, chunks *[3][3]*chunkBiomes) {
	for dx := range 3 {
		for dz := range 3 {
			chunks[dx][dz] = g.noiseBiomes(world.ChunkPos{pos[0] + int32(dx) - 1, pos[1] + int32(dz) - 1})
		}
	}
}

// quartBiomes returns the noise biomes of the quarts of the chunk at pos and
// of the quarts bordering it, read from the biomes of the chunks.
func (g *Overworld) quartBiomes(pos world.ChunkPos) vanilla.QuartBiomes {
	var chunks [3][3]*chunkBiomes
	g.loadQuartBiomes(pos, &chunks)
	return func(quartX, quartY, quartZ int32) uint8 {
		biomes := chunks[quartX>>2-pos[0]+1][quartZ>>2-pos[1]+1]
		return biomes.index[quartX&3][quartZ&3][quartY-chunkBiomeMinQuartY]
	}
}

// NoiseBiomesInChunk returns the distinct biomes NoiseBiomeAt gives
// for the quarts of the chunk at pos between minQuartY and maxQuartY.
func (s modernTerrainSource) NoiseBiomesInChunk(pos world.ChunkPos, minQuartY, maxQuartY int32) []string {
	biomes := s.generator.noiseBiomes(pos)
	var seen [256]bool
	var names []string
	for z := range 4 {
		for x := range 4 {
			for qy := minQuartY; qy <= maxQuartY; qy++ {
				index := biomes.index[x][z][max(-16, min(qy, 79))-chunkBiomeMinQuartY]
				if !seen[index] {
					seen[index] = true
					names = append(names, vanilla.OverworldBiomeName(index))
				}
			}
		}
	}
	return names
}
