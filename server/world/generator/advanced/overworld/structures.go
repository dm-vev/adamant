package overworld

import "github.com/df-mc/dragonfly/server/world/chunk"

func (g *Overworld) carve(chunkX, chunkZ int, c *chunk.Chunk) {
	// TODO: Port 1.12 MapGenCaves/MapGenRavine and apply them chunk-locally (with neighbour-start sampling)
	// to remain safe for async generation workers.
}

func (g *Overworld) generateStructures(chunkX, chunkZ int, c *chunk.Chunk) {
	// TODO: Port 1.12 structure starts (mineshafts, villages, strongholds, temples, monuments, mansions, ...)
	// and apply intersecting pieces deterministically per chunk (EndCity-style).
}
