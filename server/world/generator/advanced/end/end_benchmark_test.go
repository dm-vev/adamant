package end

import (
	"sync/atomic"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

const benchEndSeed = 12345

func findEndCityChunks(g *End) []world.ChunkPos {
	for cellX := 3; cellX <= 12; cellX++ {
		for cellZ := 3; cellZ <= 12; cellZ++ {
			startX, startZ := g.endCityStartChunk(cellX, cellZ)
			if !g.isEndCityStart(startX, startZ) {
				continue
			}
			s, err := g.endCityStructure(startX, startZ)
			if err != nil || s == nil {
				continue
			}
			minCX, maxCX := floorDiv(s.bb.minX, 16), floorDiv(s.bb.maxX, 16)
			minCZ, maxCZ := floorDiv(s.bb.minZ, 16), floorDiv(s.bb.maxZ, 16)
			positions := make([]world.ChunkPos, 0, (maxCX-minCX+1)*(maxCZ-minCZ+1))
			for cx := minCX; cx <= maxCX; cx++ {
				for cz := minCZ; cz <= maxCZ; cz++ {
					positions = append(positions, world.ChunkPos{int32(cx), int32(cz)})
				}
			}
			if len(positions) > 0 {
				return positions
			}
		}
	}
	return []world.ChunkPos{{60, 60}}
}

func endGridPositions(centres [][2]int32, radius int32) []world.ChunkPos {
	side := int(2*radius + 1)
	positions := make([]world.ChunkPos, 0, len(centres)*side*side)
	for _, ctr := range centres {
		for dx := -radius; dx <= radius; dx++ {
			for dz := -radius; dz <= radius; dz++ {
				positions = append(positions, world.ChunkPos{ctr[0] + dx, ctr[1] + dz})
			}
		}
	}
	return positions
}

func BenchmarkGenerateChunk(b *testing.B) {
	world.DefaultBlockRegistry.Finalize()
	r := world.End.Range()

	b.Run("MainIsland", func(b *testing.B) {
		positions := append(endGridPositions([][2]int32{{0, 0}}, 2), world.ChunkPos{6, 0})
		g := NewEnd(benchEndSeed)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			pos := positions[i%len(positions)]
			c := chunk.New(world.DefaultBlockRegistry, r)
			g.GenerateChunk(pos, c, world.GenerationContext{})
		}
	})

	b.Run("OuterIslands", func(b *testing.B) {
		positions := endGridPositions([][2]int32{{60, 60}, {-100, 30}}, 2)
		g := NewEnd(benchEndSeed)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			pos := positions[i%len(positions)]
			c := chunk.New(world.DefaultBlockRegistry, r)
			g.GenerateChunk(pos, c, world.GenerationContext{})
		}
	})

	b.Run("EndCity", func(b *testing.B) {
		probe := NewEnd(benchEndSeed)
		positions := findEndCityChunks(probe)
		g := NewEnd(benchEndSeed)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			pos := positions[i%len(positions)]
			c := chunk.New(world.DefaultBlockRegistry, r)
			g.GenerateChunk(pos, c, world.GenerationContext{})
		}
	})

	b.Run("ParallelOuter", func(b *testing.B) {
		positions := endGridPositions([][2]int32{{60, 60}, {-100, 30}}, 2)
		g := NewEnd(benchEndSeed)
		var idx atomic.Uint64
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				pos := positions[idx.Add(1)%uint64(len(positions))]
				c := chunk.New(world.DefaultBlockRegistry, r)
				g.GenerateChunk(pos, c, world.GenerationContext{})
			}
		})
	})
}
