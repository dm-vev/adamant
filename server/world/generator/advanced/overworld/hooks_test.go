package overworld

import (
	"bytes"
	"crypto/sha256"
	"runtime"
	"sync"
	"testing"

	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/internal/vanilla"
	"github.com/dm-vev/adamant/server/world"
	"github.com/dm-vev/adamant/server/world/biome"
	"github.com/dm-vev/adamant/server/world/chunk"
)

func TestHooksCustomizeBiomesTerrainAndDecorate(t *testing.T) {
	t.Parallel()
	world.DefaultBlockRegistry.Finalize()
	r := world.Overworld.Range()
	desertID := uint8(biome.Desert{}.EncodeBiome())
	cherryID := uint8(biome.CherryGrove{}.EncodeBiome())
	goldRID := world.BlockRuntimeID(block.Gold{})
	diamondRID := world.BlockRuntimeID(block.Diamond{})

	makeGen := func() *Overworld {
		g := NewOverworld(777)
		g.SetHooks(Hooks{
			OnBiome: func(x, y, z int, def uint8) uint8 {
				if x&1 == 0 {
					return desertID
				}
				return def
			},
			OnBiomes: func(pos world.ChunkPos, c *chunk.Chunk) {
				if uint8(c.Biome(0, 64, 0)) != desertID {
					t.Errorf("OnBiomes did not observe OnBiome override at %v", pos)
				}
				c.SetBiome(1, 64, 1, uint32(cherryID))
			},
			OnTerrain: func(pos world.ChunkPos, c *chunk.Chunk) {
				c.SetBlock(2, 250, 2, 0, goldRID)
			},
			OnDecorate: func(pos world.ChunkPos, c *chunk.Chunk, rng *vanilla.Random) {
				if c.Block(2, 250, 2, 0) != goldRID {
					t.Errorf("OnDecorate did not observe OnTerrain block at %v", pos)
				}
				h := int16(251 + rng.NextInt(10))
				for y := int16(251); y <= h; y++ {
					c.SetBlock(3, y, 3, 0, diamondRID)
				}
			},
		})
		return g
	}

	g1 := makeGen()
	g2 := makeGen()
	positions := []world.ChunkPos{{0, 0}, {1, 0}, {0, 1}, {-1, -1}, {2, -3}}

	for _, pos := range positions {
		c1 := chunk.New(world.DefaultBlockRegistry, r)
		res1 := g1.GenerateChunk(pos, c1, world.GenerationContext{})
		c2 := chunk.New(world.DefaultBlockRegistry, r)
		res2 := g2.GenerateChunk(pos, c2, world.GenerationContext{})

		if uint8(c1.Biome(0, 64, 0)) != desertID {
			t.Fatalf("chunk %v (0,64,0) biome = %d, want %d", pos, c1.Biome(0, 64, 0), desertID)
		}
		if uint8(c1.Biome(1, 64, 1)) != cherryID {
			t.Fatalf("chunk %v (1,64,1) biome = %d, want %d", pos, c1.Biome(1, 64, 1), cherryID)
		}
		if c1.Block(2, 250, 2, 0) != goldRID {
			t.Fatalf("chunk %v missing OnTerrain gold block", pos)
		}
		if c1.Block(3, 251, 3, 0) != diamondRID {
			t.Fatalf("chunk %v missing OnDecorate diamond pillar", pos)
		}
		h1 := hashChunkOutput(c1, res1, r)
		h2 := hashChunkOutput(c2, res2, r)
		if !bytes.Equal(h1[:], h2[:]) {
			t.Fatalf("hooked generation not deterministic at %v", pos)
		}
	}

	// Clearing hooks restores exact baseline vanilla output.
	baseline := NewOverworld(777)
	g1.SetHooks(Hooks{})
	if !g1.Hooks().isZero() {
		t.Fatal("expected zero Hooks after clearing")
	}
	cCleared := chunk.New(world.DefaultBlockRegistry, r)
	resCleared := g1.GenerateChunk(world.ChunkPos{0, 0}, cCleared, world.GenerationContext{})
	cBase := chunk.New(world.DefaultBlockRegistry, r)
	resBase := baseline.GenerateChunk(world.ChunkPos{0, 0}, cBase, world.GenerationContext{})
	hCleared := hashChunkOutput(cCleared, resCleared, r)
	hBase := hashChunkOutput(cBase, resBase, r)
	if !bytes.Equal(hCleared[:], hBase[:]) {
		t.Fatal("cleared hooks did not restore identical baseline chunk")
	}
}

func TestMultiWorkerAndOrderDeterminism(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	r := world.Overworld.Range()
	positions := gridPositions(10, 10)

	runSequential := func(seed int64, reverse bool) [][sha256.Size]byte {
		prevProcs := runtime.GOMAXPROCS(1)
		defer runtime.GOMAXPROCS(prevProcs)
		g := NewOverworld(seed)
		out := make([][sha256.Size]byte, len(positions))
		if !reverse {
			for i, pos := range positions {
				c := chunk.New(world.DefaultBlockRegistry, r)
				res := g.GenerateChunk(pos, c, world.GenerationContext{})
				out[i] = hashChunkOutput(c, res, r)
			}
			return out
		}
		for i := len(positions) - 1; i >= 0; i-- {
			c := chunk.New(world.DefaultBlockRegistry, r)
			res := g.GenerateChunk(positions[i], c, world.GenerationContext{})
			out[i] = hashChunkOutput(c, res, r)
		}
		return out
	}

	runMultiWorker := func(seed int64, workers int, parallel bool) [][sha256.Size]byte {
		prevProcs := runtime.GOMAXPROCS(16)
		defer runtime.GOMAXPROCS(prevProcs)
		g := NewOverworld(seed)
		g.TuneGeneration(world.GenerationTuning{Workers: workers, Parallel: parallel})
		out := make([][sha256.Size]byte, len(positions))
		jobs := make(chan int, len(positions))
		// Enqueue in scrambled stride order to vary worker completion timing.
		for step := range 5 {
			for i := step; i < len(positions); i += 5 {
				jobs <- i
			}
		}
		close(jobs)
		var wg sync.WaitGroup
		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range jobs {
					c := chunk.New(world.DefaultBlockRegistry, r)
					res := g.GenerateChunk(positions[i], c, world.GenerationContext{})
					out[i] = hashChunkOutput(c, res, r)
				}
			}()
		}
		wg.Wait()
		return out
	}

	const seed = int64(12345)
	baseline := runSequential(seed, false)
	for _, tc := range []struct {
		name string
		got  [][sha256.Size]byte
	}{
		{"1-worker-reverse", runSequential(seed, true)},
		{"8-workers-seq-origins", runMultiWorker(seed, 8, false)},
		{"8-workers-parallel-origins", runMultiWorker(seed, 8, true)},
		{"16-workers-parallel-origins", runMultiWorker(seed, 16, true)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for i, pos := range positions {
				if !bytes.Equal(tc.got[i][:], baseline[i][:]) {
					t.Fatalf("chunk %v mismatch for %s: got %x, want %x", pos, tc.name, tc.got[i], baseline[i])
				}
			}
		})
	}
}

func TestHooksConcurrentSafetyAndDeterminism(t *testing.T) {
	t.Parallel()
	world.DefaultBlockRegistry.Finalize()
	r := world.Overworld.Range()
	positions := gridPositions(6, 6)
	emeraldRID := world.BlockRuntimeID(block.Emerald{})
	meadowID := uint8(biome.Meadow{}.EncodeBiome())

	install := func(g *Overworld) {
		g.SetHooks(Hooks{
			OnBiome: func(x, y, z int, def uint8) uint8 {
				if y > 100 {
					return meadowID
				}
				return def
			},
			OnTerrain: func(pos world.ChunkPos, c *chunk.Chunk) {
				c.SetBlock(0, 260, 0, 0, emeraldRID)
			},
			OnDecorate: func(pos world.ChunkPos, c *chunk.Chunk, rng *vanilla.Random) {
				y := int16(261 + rng.NextInt(5))
				c.SetBlock(0, y, 0, 0, emeraldRID)
			},
		})
	}

	gSeq := NewOverworld(777)
	install(gSeq)
	want := make([][sha256.Size]byte, len(positions))
	for i, pos := range positions {
		c := chunk.New(world.DefaultBlockRegistry, r)
		res := gSeq.GenerateChunk(pos, c, world.GenerationContext{})
		want[i] = hashChunkOutput(c, res, r)
	}

	gConc := NewOverworld(777)
	install(gConc)
	gConc.TuneGeneration(world.GenerationTuning{Workers: 4, Parallel: true})
	got := make([][sha256.Size]byte, len(positions))
	var wg sync.WaitGroup
	for i, pos := range positions {
		wg.Add(1)
		go func(idx int, p world.ChunkPos) {
			defer wg.Done()
			c := chunk.New(world.DefaultBlockRegistry, r)
			res := gConc.GenerateChunk(p, c, world.GenerationContext{})
			got[idx] = hashChunkOutput(c, res, r)
		}(i, pos)
	}
	wg.Wait()

	for i, pos := range positions {
		if !bytes.Equal(got[i][:], want[i][:]) {
			t.Fatalf("concurrent hooked chunk %v mismatch", pos)
		}
	}
}
