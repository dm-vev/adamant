package overworld

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

const benchmarkChunkCount = 1_000

// BenchmarkGenerate1000Chunks measures full chunk generation for a fixed
// 40x25 chunk area, matching exactly 1,000 generated chunks per benchmark op.
func BenchmarkGenerate1000Chunks(b *testing.B) {
	g := NewOverworld(0)
	positions := benchmarkChunkPositions()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for _, pos := range positions {
			c := chunk.New(world.DefaultBlockRegistry, cube.Range{0, 255})
			g.GenerateChunk(pos, c)
		}
	}
}

func benchmarkChunkPositions() []world.ChunkPos {
	positions := make([]world.ChunkPos, 0, benchmarkChunkCount)
	for x := 0; x < 40; x++ {
		for z := 0; z < 25; z++ {
			positions = append(positions, world.ChunkPos{int32(x), int32(z)})
		}
	}
	return positions
}
