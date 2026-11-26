package overworld

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sync"
	"testing"

	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/internal/vanilla"
	"github.com/dm-vev/adamant/server/world"
	"github.com/dm-vev/adamant/server/world/chunk"
)

func TestNoiseBiomeBisectionMatchesBruteForce(t *testing.T) {
	seeds := []int64{12345, -987654321, 42}
	coords := [][2]int32{
		{0, 0}, {4, 4}, {16, -24}, {-64, 128}, {512, -512},
		{1600, 1600}, {-2416, 672}, {16000, -16000}, {-8000, -32000},
	}
	for _, seed := range seeds {
		noise := vanilla.NewOverworldNoise(seed)
		for _, xz := range coords {
			for dx := range int32(4) {
				for dz := range int32(4) {
					bx := xz[0] + dx*4
					bz := xz[1] + dz*4
					col := noise.SampleBiomeColumn(bx, bz)

					var got [chunkBiomeQuartsY]uint8
					var fastHint vanilla.ClimateHint
					fillNoiseBiomeColumn(col, &got, &fastHint)

					var bruteHint vanilla.ClimateHint
					for y := range int32(chunkBiomeQuartsY) {
						want := col.BiomeIndexNear((y+chunkBiomeMinQuartY)*4, &bruteHint)
						if got[y] != want {
							t.Fatalf("seed=%d pos=(%d,%d) quartY=%d: got biome %d, want %d",
								seed, bx, bz, y+chunkBiomeMinQuartY, got[y], want)
						}
					}
				}
			}
		}
	}
}

func hashChunkOutput(c *chunk.Chunk, res world.GenerationResult, r cube.Range) [sha256.Size]byte {
	h := sha256.New()
	put := func(v uint64) {
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], v)
		h.Write(b[:])
	}
	for y := int16(r.Min()); y <= int16(r.Max()); y++ {
		for x := range uint8(16) {
			for z := range uint8(16) {
				put(uint64(c.Block(x, y, z, 0)))
				put(uint64(c.Block(x, y, z, 1)))
				put(uint64(c.Biome(x, y, z)))
			}
		}
	}
	put(uint64(len(res.Entities)))
	put(uint64(len(res.BlockEntities)))
	for _, be := range res.BlockEntities {
		_, _ = fmt.Fprintf(h, "%v%v", be.Pos, be.Data)
	}
	put(uint64(len(res.ScheduledTicks)))
	for _, tick := range res.ScheduledTicks {
		_, _ = fmt.Fprintf(h, "%v", tick)
	}
	for _, s := range res.PostProcessing {
		put(uint64(len(s)))
		for _, p := range s {
			put(uint64(p))
		}
	}
	if res.SpawnOriginalMobs {
		put(1)
	}
	var sum [sha256.Size]byte
	copy(sum[:], h.Sum(nil))
	return sum
}

func TestConcurrentGenerateChunkBitIdentical(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	r := world.Overworld.Range()
	positions := gridPositions(6, 6)

	seqGen := NewOverworld(12345)
	want := make([][sha256.Size]byte, len(positions))
	for i, pos := range positions {
		c := chunk.New(world.DefaultBlockRegistry, r)
		res := seqGen.GenerateChunk(pos, c, world.GenerationContext{})
		want[i] = hashChunkOutput(c, res, r)
	}

	concGen := NewOverworld(12345)
	concGen.TuneGeneration(world.GenerationTuning{Workers: 4, Parallel: false})
	got := make([][sha256.Size]byte, len(positions))
	jobs := make(chan int, len(positions))
	for i := range positions {
		jobs <- i
	}
	close(jobs)

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				c := chunk.New(world.DefaultBlockRegistry, r)
				res := concGen.GenerateChunk(positions[i], c, world.GenerationContext{})
				got[i] = hashChunkOutput(c, res, r)
			}
		}()
	}
	wg.Wait()

	for i, pos := range positions {
		if !bytes.Equal(got[i][:], want[i][:]) {
			t.Fatalf("chunk %v hash mismatch between sequential and 4-worker concurrent generation: got %x, want %x",
				pos, got[i], want[i])
		}
	}
}
