package world

import (
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// TestHighestBlockFollowsBlockChanges checks the highest block a column
// remembers follows blocks placed above it and removed again.
func TestHighestBlockFollowsBlockChanges(t *testing.T) {
	w := Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *Tx) {
		stone, ok := w.conf.Blocks.BlockByName("minecraft:stone", nil)
		if !ok {
			t.Fatal("stone is not registered")
		}
		bottom := tx.Range()[0]
		if got := tx.HighestBlock(3, 5); got != bottom {
			t.Fatalf("highest block of an empty column %d, want %d", got, bottom)
		}
		tx.SetBlock(cube.Pos{3, 70, 5}, stone, nil)
		if got := tx.HighestBlock(3, 5); got != 70 {
			t.Fatalf("highest block %d after placing stone at 70, want 70", got)
		}
		tx.SetBlock(cube.Pos{3, 90, 5}, stone, nil)
		tx.SetBlock(cube.Pos{3, 90, 5}, nil, nil)
		if got := tx.HighestBlock(3, 5); got != 70 {
			t.Fatalf("highest block %d after removing stone at 90, want 70", got)
		}
		if got := tx.HighestBlock(4, 5); got != bottom {
			t.Fatalf("highest block of the column beside %d, want %d", got, bottom)
		}
	})
}

// pillarStructure is a structure of the block passed, two blocks wide and
// long and as high as height.
type pillarStructure struct {
	height int
	b      Block
}

func (s pillarStructure) Dimensions() [3]int { return [3]int{2, s.height, 2} }
func (s pillarStructure) At(int, int, int, func(int, int, int) Block) (Block, Liquid) {
	return s.b, nil
}

// TestHighestBlockMatchesScanningAsBlocksChange sets and removes blocks at
// random in a few columns and builds a structure over them, checking the
// highest blocks found against scanning the columns.
func TestHighestBlockMatchesScanningAsBlocksChange(t *testing.T) {
	w := Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *Tx) {
		stone, ok := w.conf.Blocks.BlockByName("minecraft:stone", nil)
		if !ok {
			t.Fatal("stone is not registered")
		}
		rng := rand.New(rand.NewPCG(1, 2))
		bottom := tx.Range()[0]
		col := tx.chunk(ChunkPos{})
		check := func(step int) {
			t.Helper()
			for x := range 4 {
				for z := range 4 {
					if got, want := tx.HighestBlock(x, z), int(col.Chunk.HighestBlock(uint8(x), uint8(z))); got != want {
						t.Fatalf("step %d: highest block of %v %v found %d, scanning finds %d", step, x, z, got, want)
					}
				}
			}
		}
		for step := range 3000 {
			pos := cube.Pos{rng.IntN(4), bottom + rng.IntN(40), rng.IntN(4)}
			if rng.IntN(2) == 0 {
				tx.SetBlock(pos, stone, nil)
			} else {
				tx.SetBlock(pos, nil, nil)
			}
			if step%7 == 0 {
				check(step)
			}
		}
		tx.BuildStructure(cube.Pos{1, bottom + 50, 1}, pillarStructure{height: 10, b: stone})
		check(-1)
	})
}

// TestEntityTickingStopsWhenANeighbourCloses checks a chunk remembered as
// entity ticking stops being so once a chunk within two of it closes, and
// one remembered as ticking its blocks once a chunk within one closes.
func TestEntityTickingStopsWhenANeighbourCloses(t *testing.T) {
	w := Config{RandomTickSpeed: -1, Provider: NopProvider{}}.New()
	defer w.Close()
	<-w.Exec(func(tx *Tx) {
		for x := int32(-2); x <= 2; x++ {
			for z := int32(-2); z <= 2; z++ {
				tx.chunk(ChunkPos{x, z})
			}
		}
		if !w.entityTickingReady(ChunkPos{}) || !w.entityTickingReady(ChunkPos{}) {
			t.Fatal("chunk with every chunk within two loaded is not entity ticking")
		}
		corner := ChunkPos{2, -2}
		w.closeChunk(tx, corner, w.chunks[corner])
		if w.entityTickingReady(ChunkPos{}) {
			t.Fatal("chunk still entity ticking after a chunk within two closed")
		}

		// Blocks tick once the chunks within one are ready, until one closes.
		if !w.generationNeighbourhoodReady(ChunkPos{}) || !w.generationNeighbourhoodReady(ChunkPos{}) {
			t.Fatal("blocks of a chunk with every chunk within one loaded do not tick")
		}
		beside := ChunkPos{1, 1}
		w.closeChunk(tx, beside, w.chunks[beside])
		if w.generationNeighbourhoodReady(ChunkPos{}) {
			t.Fatal("blocks of a chunk still tick after a chunk within one closed")
		}
	})
}
