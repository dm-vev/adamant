package overworld

import "github.com/df-mc/dragonfly/server/world/chunk"

func (g *Overworld) carve(chunkX, chunkZ int, c *chunk.Chunk, biomes []*biomeDef) {
	g.carveCaves(chunkX, chunkZ, c, biomes)
	g.carveRavines(chunkX, chunkZ, c, biomes)
}

func (g *Overworld) generateStructures(chunkX, chunkZ int, c *chunk.Chunk) {
	g.applyScatteredStructures(chunkX, chunkZ, c)

	// TODO: Port remaining 1.12 structure starts (mineshafts, villages, strongholds, temples, monuments, mansions, ...).
}
