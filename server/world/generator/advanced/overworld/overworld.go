package overworld

import (
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

const (
	seaLevel = 63
)

// Overworld generates Java 26.3 terrain and features with Bedrock block states.
// Immutable terrain previews and worker-local samplers permit concurrent use.
type Overworld struct {
	seed             int64
	noise            *vanilla.OverworldNoise
	zoomSeed         int64
	modernSurface    *modernSurface
	modernStructures *modernStructures
	modernFeatures   *ModernFeatures
	rawCache         *previewCache
	modernPool       sync.Pool
	motionBlocking   sync.Map // native runtime ID -> Java heightmap tag membership

	// cached runtime IDs
	airRID   uint32
	stoneRID uint32
	waterRID uint32
	lavaRID  uint32

	previewCache *previewCache
	chunkBiomes  chunkBiomeCache
}

// previewCacheSize bounds the surfaced terrain previews kept for decoration,
// which reads the terrain up to three chunks from the chunk it decorates:
// generation spreading out from a player reads some 1400 chunks at a view
// distance of 32 chunks. A preview evicted too early is built again.
const previewCacheSize = 2048

// New returns a Java 26.3 Overworld generator using the provided seed.
func New(seed int64) *Overworld {
	return NewOverworld(seed)
}

// NewOverworld returns a Java 26.3 Overworld generator using the provided seed.
func NewOverworld(seed int64) *Overworld {
	g := &Overworld{
		seed:         seed,
		noise:        vanilla.NewOverworldNoise(seed),
		zoomSeed:     vanilla.BiomeZoomSeed(seed),
		airRID:       world.BlockRuntimeID(block.Air{}),
		stoneRID:     world.BlockRuntimeID(block.Stone{}),
		waterRID:     world.BlockRuntimeID(block.Water{Depth: 8, Still: true}),
		lavaRID:      world.BlockRuntimeID(block.Lava{Depth: 8, Still: true}),
		rawCache:     newPreviewCache(128),
		previewCache: newPreviewCache(previewCacheSize),
	}
	g.modernPool.New = func() any { return new(modernTerrainScratch) }
	g.modernSurface = newModernSurface(seed)
	g.modernStructures = newModernStructures(seed, modernTerrainSource{generator: g, raw: true})
	g.modernFeatures = NewModernFeatures(seed, modernTerrainSource{generator: g})
	return g
}

// GenerateChunk generates a single overworld chunk at the position passed.
func (g *Overworld) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk, context world.GenerationContext) world.GenerationResult {
	base := g.terrainColumn(pos, false)
	*c = *base.c.Clone()
	result := g.modernFeatures.Decorate(pos, c, context)
	decorationMarkers := result.PostProcessing
	result.PostProcessing = nil
	appendPostProcessing(&result, base.postProcessing)
	appendPostProcessing(&result, decorationMarkers)
	result.SpawnOriginalMobs = true
	return result
}

// TuneGeneration sizes the caches of the generator for the workers of the
// World and sets whether a chunk computes its neighbourhood in parallel.
func (g *Overworld) TuneGeneration(tuning world.GenerationTuning) {
	g.previewCache = newPreviewCache(max(previewCacheSize, 128*tuning.Workers))
	g.modernFeatures.Tune(tuning.Workers, tuning.Parallel)
}

// DefaultSpawn returns the default overworld spawn position.
func (*Overworld) DefaultSpawn(world.Dimension) cube.Pos {
	return cube.Pos{0, seaLevel + 2, 0}
}
