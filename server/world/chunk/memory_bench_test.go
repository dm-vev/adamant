package chunk_test

import (
	"bytes"
	"math/rand/v2"
	"runtime"
	"sync"
	"testing"

	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/world"
	"github.com/dm-vev/adamant/server/world/chunk"
)

// buildTypicalOverworldColumn constructs a deterministic 24-subchunk Overworld column
// (-64..319): ~8 underground stone/deepslate/ore/cave subchunks, ~2 surface subchunks
// with dirt/grass/water/leaves/emitters, ~14 empty sky air subchunks, and realistic biomes.
func buildTypicalOverworldColumn(seed uint64) *chunk.Chunk {
	world.DefaultBlockRegistry.Finalize()
	br := world.DefaultBlockRegistry
	r := world.Overworld.Range()
	c := chunk.New(br, r)

	rng := rand.New(rand.NewPCG(seed, seed*31+7))
	rid := world.BlockRuntimeID
	bedrock := rid(block.Bedrock{})
	deepslate := rid(block.Deepslate{})
	stone := rid(block.Stone{})
	granite := rid(block.Granite{})
	diorite := rid(block.Diorite{})
	andesite := rid(block.Andesite{})
	gravel := rid(block.Gravel{})
	dirt := rid(block.Dirt{})
	grass := rid(block.Grass{})
	water := rid(block.Water{Depth: 8, Still: true})
	coalOre := rid(block.CoalOre{})
	ironOre := rid(block.IronOre{})
	copperOre := rid(block.CopperOre{})
	lava := rid(block.Lava{Depth: 8, Still: true})
	leaves := rid(block.Leaves{Type: block.OakLeaves()})
	oakLog := rid(block.Log{Wood: block.OakWood()})

	// Underground & surface blocks: Y = -64 .. 80 (subchunks 0..8 underground, 9 surface, 10..23 empty sky).
	for x := range uint8(16) {
		for z := range uint8(16) {
			surface := 64 + rng.IntN(10)
			for y := r.Min(); y <= surface; y++ {
				var b uint32
				switch {
				case y == r.Min():
					b = bedrock
				case y < -56 && rng.IntN(20) == 0:
					b = lava
				case y > -40 && y < 32 && rng.IntN(10) == 0:
					// Caves (leave air).
					continue
				case y < 0:
					switch rng.IntN(16) {
					case 0:
						b = ironOre
					case 1:
						b = gravel
					default:
						b = deepslate
					}
				case y < surface-4:
					switch rng.IntN(16) {
					case 0:
						b = granite
					case 1:
						b = diorite
					case 2:
						b = andesite
					case 3:
						b = coalOre
					case 4:
						b = copperOre
					case 5:
						b = gravel
					default:
						b = stone
					}
				case y < surface:
					b = dirt
				default:
					b = grass
				}
				c.SetBlock(x, int16(y), z, 0, b)
			}
			// Occasional water pocket & tree canopy on surface.
			if surface < 67 {
				for y := surface + 1; y <= 67; y++ {
					c.SetBlock(x, int16(y), z, 0, water)
				}
			} else if x >= 4 && x <= 7 && z >= 4 && z <= 7 {
				c.SetBlock(x, int16(surface+1), z, 0, oakLog)
				c.SetBlock(x, int16(surface+2), z, 0, leaves)
				c.SetBlock(x, int16(surface+3), z, 0, leaves)
			}
		}
	}

	// Realistic 3D biomes: plains (1) + forest (4) on surface/sky, dripstone caves (188) underground.
	for y := int16(r.Min()); y <= int16(r.Max()); y++ {
		for x := range uint8(16) {
			for z := range uint8(16) {
				b := uint32(1)
				if x >= 10 {
					b = 4
				}
				if y < 0 && x < 8 && z < 8 {
					b = 188
				}
				c.SetBiome(x, y, z, b)
			}
		}
	}

	c.Compact()
	chunk.LightArea([]*chunk.Chunk{c}, 0, 0).Fill()
	return c
}

func measureLiveHeapPerChunk(t *testing.T, count int, makeChunk func(i int) *chunk.Chunk) float64 {
	t.Helper()
	// Warm up any one-time initialization before measuring.
	_ = makeChunk(0)
	runtime.GC()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	keep := make([]*chunk.Chunk, count)
	for i := range keep {
		keep[i] = makeChunk(i + 1)
	}
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(keep)

	if after.HeapAlloc <= before.HeapAlloc {
		return 0
	}
	return float64(after.HeapAlloc-before.HeapAlloc) / float64(count)
}

// TestColumnHeapFootprint reports live heap bytes per column for key chunk lifecycles.
func TestColumnHeapFootprint(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	br := world.DefaultBlockRegistry
	r := world.Overworld.Range()

	const n = 128

	newBytes := measureLiveHeapPerChunk(t, n, func(int) *chunk.Chunk {
		return chunk.New(br, r)
	})
	overworldBytes := measureLiveHeapPerChunk(t, n, func(i int) *chunk.Chunk {
		return buildTypicalOverworldColumn(uint64(i))
	})

	sample := buildTypicalOverworldColumn(42)
	encoded := chunk.Encode(sample, chunk.DiskEncoding)
	decodedUnlitBytes := measureLiveHeapPerChunk(t, n, func(int) *chunk.Chunk {
		c, err := chunk.DiskDecode(br, encoded, r)
		if err != nil {
			t.Fatal(err)
		}
		return c
	})
	decodedLitBytes := measureLiveHeapPerChunk(t, n, func(int) *chunk.Chunk {
		c, err := chunk.DiskDecode(br, encoded, r)
		if err != nil {
			t.Fatal(err)
		}
		chunk.LightArea([]*chunk.Chunk{c}, 0, 0).Fill()
		return c
	})
	cloneBlocksBytes := measureLiveHeapPerChunk(t, n, func(int) *chunk.Chunk {
		return sample.CloneBlocks()
	})
	cloneFullBytes := measureLiveHeapPerChunk(t, n, func(int) *chunk.Chunk {
		return sample.Clone()
	})

	t.Logf("Live heap per 24-subchunk Overworld column (N=%d):", n)
	t.Logf("  chunk.New:               %7.2f KiB (%7.0f B)", newBytes/1024, newBytes)
	t.Logf("  Typical Overworld (lit): %7.2f KiB (%7.0f B)", overworldBytes/1024, overworldBytes)
	t.Logf("  DiskDecode (unlit):      %7.2f KiB (%7.0f B)", decodedUnlitBytes/1024, decodedUnlitBytes)
	t.Logf("  DiskDecode (lit):        %7.2f KiB (%7.0f B)", decodedLitBytes/1024, decodedLitBytes)
	t.Logf("  CloneBlocks:             %7.2f KiB (%7.0f B)", cloneBlocksBytes/1024, cloneBlocksBytes)
	t.Logf("  Clone (lit):             %7.2f KiB (%7.0f B)", cloneFullBytes/1024, cloneFullBytes)
}

func BenchmarkChunkNew(b *testing.B) {
	world.DefaultBlockRegistry.Finalize()
	br := world.DefaultBlockRegistry
	r := world.Overworld.Range()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = chunk.New(br, r)
	}
}

func BenchmarkDiskEncode(b *testing.B) {
	c := buildTypicalOverworldColumn(1)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = chunk.Encode(c, chunk.DiskEncoding)
	}
}

func BenchmarkDiskDecode(b *testing.B) {
	world.DefaultBlockRegistry.Finalize()
	br := world.DefaultBlockRegistry
	r := world.Overworld.Range()
	c := buildTypicalOverworldColumn(1)
	data := chunk.Encode(c, chunk.DiskEncoding)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		decoded, err := chunk.DiskDecode(br, data, r)
		if err != nil {
			b.Fatal(err)
		}
		_ = decoded
	}
}

func BenchmarkChunkCloneBlocks(b *testing.B) {
	c := buildTypicalOverworldColumn(1)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = c.CloneBlocks()
	}
}

func BenchmarkChunkCompact(b *testing.B) {
	base := buildTypicalOverworldColumn(1)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		c := base.Clone()
		b.StartTimer()
		c.Compact()
	}
}

func BenchmarkSubChunkBlock(b *testing.B) {
	c := buildTypicalOverworldColumn(1)
	sub := c.SubChunk(0)
	var sink uint32
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		x := byte(i & 15)
		y := byte((i >> 4) & 15)
		z := byte((i >> 8) & 15)
		sink ^= sub.Block(x, y, z, 0)
	}
	_ = sink
}

func BenchmarkSubChunkSetBlock(b *testing.B) {
	c := buildTypicalOverworldColumn(1)
	sub := c.SubChunk(0)
	stone := world.BlockRuntimeID(block.Stone{})
	dirt := world.BlockRuntimeID(block.Dirt{})
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		x := byte(i & 15)
		y := byte((i >> 4) & 15)
		z := byte((i >> 8) & 15)
		if i&1 == 0 {
			sub.SetBlock(x, y, z, 0, stone)
		} else {
			sub.SetBlock(x, y, z, 0, dirt)
		}
	}
}

func BenchmarkSubChunkSkyLight(b *testing.B) {
	c := buildTypicalOverworldColumn(1)
	sub := c.SubChunk(64)
	var sink uint8
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		x := byte(i & 15)
		y := byte((i >> 4) & 15)
		z := byte((i >> 8) & 15)
		sink ^= sub.SkyLight(x, y, z)
	}
	_ = sink
}

func BenchmarkSubChunkSetSkyLight(b *testing.B) {
	c := buildTypicalOverworldColumn(1)
	sub := c.SubChunk(64)
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		x := byte(i & 15)
		y := byte((i >> 4) & 15)
		z := byte((i >> 8) & 15)
		sub.SetSkyLight(x, y, z, uint8(i&15))
	}
}

func BenchmarkFillBiomes2D(b *testing.B) {
	world.DefaultBlockRegistry.Finalize()
	br := world.DefaultBlockRegistry
	r := world.Overworld.Range()
	grid := make([]uint32, 256)
	for i := range grid {
		if i%2 == 0 {
			grid[i] = 1
		} else {
			grid[i] = 4
		}
	}
	c := chunk.New(br, r)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		c.FillBiomes2D(grid)
	}
}

func BenchmarkLightAreaFill(b *testing.B) {
	base := buildTypicalOverworldColumn(1)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		c := base.CloneBlocks()
		b.StartTimer()
		chunk.LightArea([]*chunk.Chunk{c}, 0, 0).Fill()
	}
}

func TestSharedBiomeCopyOnWriteAndConcurrency(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	br := world.DefaultBlockRegistry
	r := world.Overworld.Range()

	// Exercise FillBiomes2D across 0, 1, 2, 3, 4, 5, and 8 bits per index.
	for _, unique := range []int{1, 2, 4, 7, 16, 20, 70} {
		c := chunk.New(br, r)
		grid := make([]uint32, 256)
		for i := range grid {
			if unique == 1 {
				grid[i] = 0
			} else {
				grid[i] = uint32(i%unique) + 1
			}
		}
		c.FillBiomes2D(grid)
		for x := range uint8(16) {
			for z := range uint8(16) {
				want := grid[int(x)+int(z)*16]
				for _, y := range []int16{int16(r.Min()), 0, 64, int16(r.Max())} {
					if got := c.Biome(x, y, z); got != want {
						t.Fatalf("unique=%d biome(%d,%d,%d) = %d, want %d", unique, x, y, z, got, want)
					}
				}
			}
		}
		// COW mutation in one subchunk must not affect other subchunks or clones.
		clone := c.Clone()
		cloneBlocks := c.CloneBlocks()
		c.SetBiome(3, 64, 5, 99)
		if got := c.Biome(3, 64, 5); got != 99 {
			t.Fatalf("mutated biome = %d, want 99", got)
		}
		if got := c.Biome(3, 80, 5); got != grid[3+5*16] {
			t.Fatalf("sibling subchunk biome = %d, want %d", got, grid[3+5*16])
		}
		if got := clone.Biome(3, 64, 5); got != grid[3+5*16] {
			t.Fatalf("clone biome mutated to %d, want %d", got, grid[3+5*16])
		}
		if got := cloneBlocks.Biome(3, 64, 5); got != grid[3+5*16] {
			t.Fatalf("cloneBlocks biome mutated to %d, want %d", got, grid[3+5*16])
		}
	}

	// Concurrent reads and COW mutations across goroutines under -race.
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			c := chunk.New(br, r)
			if got := c.Biome(0, 0, 0); got != 0 {
				t.Errorf("fresh biome = %d, want 0", got)
			}
			c.SetBiome(0, 0, 0, 0) // no-op on shared zeroBiomeStorage
			c.SetBiome(1, int16(id*16), 2, uint32(id+10))
			c.Compact()
			cloned := c.Clone()
			cloned.SetBiome(1, int16(id*16), 2, 200)
			if got := c.Biome(1, int16(id*16), 2); got != uint32(id+10) {
				t.Errorf("original mutated to %d, want %d", got, id+10)
			}
			enc := chunk.Encode(c, chunk.DiskEncoding)
			dec, err := chunk.DiskDecode(br, enc, r)
			if err != nil {
				t.Errorf("DiskDecode: %v", err)
				return
			}
			if got := dec.Biome(1, int16(id*16), 2); got != uint32(id+10) {
				t.Errorf("decoded biome = %d, want %d", got, id+10)
			}
		}(g)
	}
	wg.Wait()
}

func TestUniformLightCOWAndCompaction(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	br := world.DefaultBlockRegistry
	r := world.Overworld.Range()

	c := chunk.New(br, r)
	chunk.LightArea([]*chunk.Chunk{c}, 0, 0).Fill()

	// Sky subchunk should be full sky light (15) and zero block light (0).
	sub := c.SubChunk(128)
	if sub.SkyLight(4, 5, 6) != 15 || sub.BlockLight(4, 5, 6) != 0 {
		t.Fatalf("unexpected initial light: sky=%d block=%d", sub.SkyLight(4, 5, 6), sub.BlockLight(4, 5, 6))
	}
	// No-op uniform writes must not allocate or corrupt shared arrays.
	sub.SetSkyLight(4, 5, 6, 15)
	sub.SetBlockLight(4, 5, 6, 0)

	// Non-uniform write must COW cleanly without affecting other subchunks.
	sub.SetSkyLight(4, 5, 6, 7)
	sub.SetBlockLight(4, 5, 6, 11)
	if sub.SkyLight(4, 5, 6) != 7 || sub.SkyLight(4, 5, 7) != 15 {
		t.Fatalf("sky light after COW: (%d, %d)", sub.SkyLight(4, 5, 6), sub.SkyLight(4, 5, 7))
	}
	if sub.BlockLight(4, 5, 6) != 11 || sub.BlockLight(4, 5, 7) != 0 {
		t.Fatalf("block light after COW: (%d, %d)", sub.BlockLight(4, 5, 6), sub.BlockLight(4, 5, 7))
	}
	if c.SubChunk(144).SkyLight(4, 5, 6) != 15 || c.SubChunk(144).BlockLight(4, 5, 6) != 0 {
		t.Fatalf("sibling subchunk light corrupted")
	}

	// Restoring uniform values + Compact() must preserve all values.
	sub.SetSkyLight(4, 5, 6, 15)
	sub.SetBlockLight(4, 5, 6, 0)
	c.Compact()
	for x := range byte(16) {
		for y := range byte(16) {
			for z := range byte(16) {
				if sub.SkyLight(x, y, z) != 15 || sub.BlockLight(x, y, z) != 0 {
					t.Fatalf("post-compact light mismatch at (%d,%d,%d)", x, y, z)
				}
			}
		}
	}
}

func TestDiskEncodeDecodeRoundTripAllFormats(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	br := world.DefaultBlockRegistry
	r := world.Overworld.Range()

	c := buildTypicalOverworldColumn(7)
	// Add a second layer (waterlogged block) and a subchunk with >16 distinct block states (5-bit palette).
	water := world.BlockRuntimeID(block.Water{Depth: 8, Still: true})
	c.SetBlock(2, 65, 3, 1, water)
	for i := range 25 {
		c.SetBlock(uint8(i%16), 100, uint8(i/16), 0, uint32(i+1))
	}
	c.Compact()

	encoded1 := chunk.Encode(c, chunk.DiskEncoding)
	decoded, err := chunk.DiskDecode(br, encoded1, r)
	if err != nil {
		t.Fatalf("DiskDecode failed: %v", err)
	}
	if !c.Equals(decoded) {
		t.Fatalf("decoded chunk not equal to original")
	}
	for y := int16(r.Min()); y <= int16(r.Max()); y += 16 {
		for x := range uint8(16) {
			for z := range uint8(16) {
				if c.Biome(x, y, z) != decoded.Biome(x, y, z) {
					t.Fatalf("biome mismatch at (%d,%d,%d): got %d want %d", x, y, z, decoded.Biome(x, y, z), c.Biome(x, y, z))
				}
			}
		}
	}
	encoded2 := chunk.Encode(decoded, chunk.DiskEncoding)
	if !bytes.Equal(encoded1.Biomes, encoded2.Biomes) {
		t.Fatalf("re-encoded biome bytes differ")
	}
	for i := range encoded1.SubChunks {
		if len(encoded1.SubChunks[i]) != len(encoded2.SubChunks[i]) {
			t.Fatalf("subchunk %d re-encoded length differs: %d vs %d", i, len(encoded1.SubChunks[i]), len(encoded2.SubChunks[i]))
		}
	}
}
