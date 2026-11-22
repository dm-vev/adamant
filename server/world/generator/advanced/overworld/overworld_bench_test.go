package overworld

import (
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

func gridPositions(width, height int32) []world.ChunkPos {
	positions := make([]world.ChunkPos, 0, int(width)*int(height))
	for x := range width {
		for z := range height {
			positions = append(positions, world.ChunkPos{x, z})
		}
	}
	return positions
}

func benchmarkOverworldGrid(b *testing.B, width, height int32, workers int) {
	world.DefaultBlockRegistry.Finalize()
	positions := gridPositions(width, height)
	r := world.Overworld.Range()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		g := NewOverworld(12345)
		if workers > 1 {
			g.TuneGeneration(world.GenerationTuning{Workers: workers, Parallel: false})
		}
		b.StartTimer()
		if workers <= 1 {
			for _, pos := range positions {
				c := chunk.New(world.DefaultBlockRegistry, r)
				g.GenerateChunk(pos, c, world.GenerationContext{})
			}
		} else {
			ch := make(chan world.ChunkPos, len(positions))
			for _, pos := range positions {
				ch <- pos
			}
			close(ch)
			var wg sync.WaitGroup
			for range workers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for pos := range ch {
						c := chunk.New(world.DefaultBlockRegistry, r)
						g.GenerateChunk(pos, c, world.GenerationContext{})
					}
				}()
			}
			wg.Wait()
		}
	}
	b.StopTimer()
	totalChunks := float64(b.N * len(positions))
	elapsedSec := b.Elapsed().Seconds()
	if elapsedSec > 0 && totalChunks > 0 {
		b.ReportMetric((elapsedSec*1000.0)/totalChunks, "ms/chunk")
		b.ReportMetric(totalChunks/elapsedSec, "col/s")
	}
}

func BenchmarkGenerateOverworld8x8(b *testing.B) {
	benchmarkOverworldGrid(b, 8, 8, 1)
}

func BenchmarkGenerateOverworld16x16(b *testing.B) {
	benchmarkOverworldGrid(b, 16, 16, 1)
}

func BenchmarkGenerateOverworld16x16_2Workers(b *testing.B) {
	benchmarkOverworldGrid(b, 16, 16, 2)
}
