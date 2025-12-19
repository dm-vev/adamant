package nether

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
)

type netherPreview struct {
	chunk            *chunk.Chunk
	randAfterSurface mc112.Rand
}

func (g *Nether) previewChunk(chunkX, chunkZ int) *netherPreview {
	c := chunk.New(g.airRID, cube.Range{0, 127})

	s := g.pool.Get().(*netherScratch)
	defer g.pool.Put(s)

	r := mc112.NewRand(int64(chunkX)*341873128712 + int64(chunkZ)*132897987541)
	g.prepareHeights(chunkX, chunkZ, c, s)
	g.buildSurfaces(chunkX, chunkZ, c, r, s)

	randAfter := *r

	g.carveCaves(chunkX, chunkZ, c)
	// Structures are placed here once ported.

	return &netherPreview{chunk: c, randAfterSurface: randAfter}
}

func (g *Nether) previewChunkCached(preview map[world.ChunkPos]*netherPreview, chunkX, chunkZ int) *netherPreview {
	pos := world.ChunkPos{int32(chunkX), int32(chunkZ)}
	if p, ok := preview[pos]; ok {
		return p
	}
	p := g.previewChunk(chunkX, chunkZ)
	preview[pos] = p
	return p
}

func (g *Nether) blockRIDAt(c *chunk.Chunk, preview map[world.ChunkPos]*netherPreview, chunkX, chunkZ int, worldX, worldY, worldZ int) uint32 {
	if worldY < 0 || worldY > 127 {
		return g.airRID
	}
	if worldX>>4 == chunkX && worldZ>>4 == chunkZ {
		return c.Block(uint8(worldX&15), int16(worldY), uint8(worldZ&15), 0)
	}
	p := g.previewChunkCached(preview, worldX>>4, worldZ>>4)
	return p.chunk.Block(uint8(worldX&15), int16(worldY), uint8(worldZ&15), 0)
}

func (g *Nether) setRIDIfInChunk(c *chunk.Chunk, chunkX, chunkZ int, worldX, worldY, worldZ int, rid uint32) {
	if worldY < 0 || worldY > 127 {
		return
	}
	if worldX>>4 != chunkX || worldZ>>4 != chunkZ {
		return
	}
	yy := int16(worldY)
	if yy < int16(c.Range().Min()) || yy > int16(c.Range().Max()) {
		return
	}
	c.SetBlock(uint8(worldX&15), yy, uint8(worldZ&15), 0, rid)
}
