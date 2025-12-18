package overworld

import (
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
)

func (g *Overworld) carve(chunkX, chunkZ int, c *chunk.Chunk, biomes []*biomeDef) {
	g.carveCaves(chunkX, chunkZ, c, biomes)
	g.carveRavines(chunkX, chunkZ, c, biomes)
}

// generateStructures matches the "map features" part of ChunkGeneratorOverworld.populate. It is intentionally
// passed the populate RNG so future ports can match vanilla RNG consumption.
//
// The returned bool indicates whether a village was generated (vanilla uses this to suppress some lakes).
func (g *Overworld) generateStructures(chunkX, chunkZ int, c *chunk.Chunk, r *mc112.Rand) (villageGenerated bool) {
	_ = r
	g.applyScatteredStructures(chunkX, chunkZ, c)

	// TODO: Port remaining 1.12 structure starts (mineshafts, villages, strongholds, monuments, mansions, ...).
	return false
}
