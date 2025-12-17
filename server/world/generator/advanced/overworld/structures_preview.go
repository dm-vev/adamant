package overworld

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
)

func (g *Overworld) previewChunk(chunkX, chunkZ int) *chunk.Chunk {
	c := chunk.New(g.airRID, cube.Range{0, 255})

	s := g.pool.Get().(*scratch)
	defer g.pool.Put(s)

	genIDs := g.biomeProvider.biomesForGeneration(chunkX*4-2, chunkZ*4-2, 10, 10)
	biomeIDs := g.biomeProvider.biomes(chunkX*16, chunkZ*16, 16, 16)

	var biomesForGeneration [10 * 10]*biomeDef
	var biomes [16 * 16]*biomeDef
	for i, id := range genIDs {
		biomesForGeneration[i] = g.biomeDef(id)
	}
	for i, id := range biomeIDs {
		biomes[i] = g.biomeDef(id)
	}

	g.setBlocksInChunk(chunkX, chunkZ, c, biomesForGeneration[:], s)
	r := mc112.NewRand(int64(chunkX)*341873128712 + int64(chunkZ)*132897987541)
	g.replaceBiomeBlocks(chunkX, chunkZ, c, biomes[:], r, s)
	return c
}

func (g *Overworld) previewSurfaceY(worldX, worldZ int) int {
	chunkX, chunkZ := worldX>>4, worldZ>>4
	c := g.previewChunk(chunkX, chunkZ)
	return int(c.HighestBlock(uint8(worldX&15), uint8(worldZ&15)))
}
