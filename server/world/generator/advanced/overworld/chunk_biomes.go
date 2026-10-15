package overworld

import (
	"sync"

	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
)

// Noise biomes are sampled at quart heights -16 to 79; JavaNoiseBiomeAt
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

// chunkBiomeCache keeps the noise biomes of recently decorated chunks: every
// chunk is read by the decoration of the nine chunks around it.
type chunkBiomeCache struct {
	shards [chunkBiomeShards]struct {
		mu     sync.Mutex
		biomes map[world.ChunkPos]*chunkBiomes
	}
}

// get returns the cached noise biomes of the chunk, if any.
func (c *chunkBiomeCache) get(pos world.ChunkPos) (*chunkBiomes, bool) {
	h := uint32(pos[0])*0x9e3779b1 ^ uint32(pos[1])*0x85ebca77
	shard := &c.shards[(h^h>>15)%chunkBiomeShards]
	shard.mu.Lock()
	biomes, ok := shard.biomes[pos]
	shard.mu.Unlock()
	return biomes, ok
}

func (c *chunkBiomeCache) load(pos world.ChunkPos, compute func() *chunkBiomes) *chunkBiomes {
	if biomes, ok := c.get(pos); ok {
		return biomes
	}
	h := uint32(pos[0])*0x9e3779b1 ^ uint32(pos[1])*0x85ebca77
	shard := &c.shards[(h^h>>15)%chunkBiomeShards]
	biomes := compute()
	shard.mu.Lock()
	if shard.biomes == nil || len(shard.biomes) >= chunkBiomeShardSize {
		shard.biomes = make(map[world.ChunkPos]*chunkBiomes, chunkBiomeShardSize)
	}
	shard.biomes[pos] = biomes
	shard.mu.Unlock()
	return biomes
}

// noiseBiomes returns the noise biomes of the chunk, sampling each of its
// sixteen quart columns once.
func (g *Overworld) noiseBiomes(pos world.ChunkPos) *chunkBiomes {
	return g.chunkBiomes.load(pos, func() *chunkBiomes {
		biomes := &chunkBiomes{}
		var hint java26.ClimateHint
		for z := range int32(4) {
			for x := range int32(4) {
				column := g.noise.SampleBiomeColumn((pos[0]*4+x)*4, (pos[1]*4+z)*4)
				for y := range int32(chunkBiomeQuartsY) {
					biomes.index[x][z][y] = column.BiomeIndexNear((y+chunkBiomeMinQuartY)*4, &hint)
				}
			}
		}
		return biomes
	})
}

// quartBiomes returns the noise biomes of the quarts of the chunk at pos and
// of the quarts bordering it, read from the biomes of the chunks.
func (g *Overworld) quartBiomes(pos world.ChunkPos) java26.QuartBiomes {
	var chunks [3][3]*chunkBiomes
	for dx := range 3 {
		for dz := range 3 {
			chunks[dx][dz] = g.noiseBiomes(world.ChunkPos{pos[0] + int32(dx) - 1, pos[1] + int32(dz) - 1})
		}
	}
	return func(quartX, quartY, quartZ int32) uint8 {
		biomes := chunks[quartX>>2-pos[0]+1][quartZ>>2-pos[1]+1]
		return biomes.index[quartX&3][quartZ&3][quartY-chunkBiomeMinQuartY]
	}
}

// JavaNoiseBiomesInChunk returns the distinct biomes JavaNoiseBiomeAt gives
// for the quarts of the chunk at pos between minQuartY and maxQuartY.
func (s modernTerrainSource) JavaNoiseBiomesInChunk(pos world.ChunkPos, minQuartY, maxQuartY int32) []string {
	biomes := s.generator.noiseBiomes(pos)
	var seen [256]bool
	var names []string
	for z := range 4 {
		for x := range 4 {
			for qy := minQuartY; qy <= maxQuartY; qy++ {
				index := biomes.index[x][z][max(-16, min(qy, 79))-chunkBiomeMinQuartY]
				if !seen[index] {
					seen[index] = true
					names = append(names, java26.OverworldBiomeName(index))
				}
			}
		}
	}
	return names
}
