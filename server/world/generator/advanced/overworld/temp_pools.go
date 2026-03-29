package overworld

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

func (g *Overworld) acquirePreviewScratch() map[world.ChunkPos]*chunk.Chunk {
	if v := g.previewScratch.Get(); v != nil {
		return v.(map[world.ChunkPos]*chunk.Chunk)
	}
	return make(map[world.ChunkPos]*chunk.Chunk, 9)
}

func (g *Overworld) releasePreviewScratch(m map[world.ChunkPos]*chunk.Chunk) {
	for k := range m {
		delete(m, k)
	}
	g.previewScratch.Put(m)
}

func (g *Overworld) acquireLakeShape() []bool {
	if v := g.lakeShapePool.Get(); v != nil {
		return v.([]bool)
	}
	return make([]bool, 16*16*8)
}

func (g *Overworld) releaseLakeShape(shape []bool) {
	clear(shape)
	g.lakeShapePool.Put(shape)
}
