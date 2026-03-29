package world

import (
	"math"
	"testing"
)

func BenchmarkPopulateLoadQueue(b *testing.B) {
	const radius = 17

	b.Run("legacy", func(b *testing.B) {
		loader := &Loader{
			r:      radius,
			pos:    ChunkPos{128, -64},
			loaded: make(map[ChunkPos]*Column),
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			loader.loadQueue = legacyLoadQueue(loader.r, loader.pos, loader.loaded, loader.loadQueue[:0])
		}
	})

	b.Run("cached_offsets", func(b *testing.B) {
		loader := &Loader{
			r:      radius,
			pos:    ChunkPos{128, -64},
			loaded: make(map[ChunkPos]*Column),
		}
		_ = loaderOffsets(radius)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			loader.populateLoadQueue()
		}
	})
}

func legacyLoadQueue(radius int, pos ChunkPos, loaded map[ChunkPos]*Column, dst []ChunkPos) []ChunkPos {
	queue := make(map[int32][]ChunkPos, radius+1)

	r := int32(radius)
	for x := -r; x <= r; x++ {
		for z := -r; z <= r; z++ {
			distance := math.Sqrt(float64(x*x) + float64(z*z))
			chunkDistance := int32(math.Round(distance))
			if chunkDistance > r {
				continue
			}
			chunkPos := ChunkPos{x + pos[0], z + pos[1]}
			if _, ok := loaded[chunkPos]; ok {
				continue
			}
			queue[chunkDistance] = append(queue[chunkDistance], chunkPos)
		}
	}

	dst = dst[:0]
	for i := int32(0); i <= r; i++ {
		dst = append(dst, queue[i]...)
	}
	return dst
}
