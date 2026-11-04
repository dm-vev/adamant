package world_test

import (
	"fmt"
	"log/slog"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator"
)

// TestSaveChunksStoresGeneratedAndLoadedColumns generates a row of columns
// that nothing changes, then saves, unloads, closes and loads them again,
// through the background saver or not, and counts the stores of each column.
// Both modes store a column generated as soon as it is first saved, saved or
// unloaded: it is not on disk yet. ChunkSavingAll stores every column again at
// every save, unload and close, also those read back from the provider;
// ChunkSavingModified stores none of them again.
func TestSaveChunksStoresGeneratedAndLoadedColumns(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	gen := generator.NewFlat(biome.Plains{}, []world.Block{block.Grass{}, block.Dirt{}, block.Dirt{}, block.Bedrock{}})
	const n = 6
	var columns []world.ChunkPos
	for x := range int32(n) {
		columns = append(columns, world.ChunkPos{x, 0})
	}
	// bottom is the lowest block of the column at pos, where the generator
	// places bedrock.
	bottom := func(pos world.ChunkPos) cube.Pos {
		return cube.Pos{int(pos[0])*16 + 1, world.Overworld.Range().Min(), 1}
	}
	load := func(tx *world.Tx) {
		for _, pos := range columns {
			_ = tx.Block(bottom(pos))
		}
	}
	for _, mode := range []world.ChunkSaving{world.ChunkSavingModified, world.ChunkSavingAll} {
		for _, synchronous := range []bool{true, false} {
			t.Run(fmt.Sprintf("%v/synchronous=%v", mode, synchronous), func(t *testing.T) {
				t.Parallel()
				p := newCountingStore()
				newWorld := func() *world.World {
					return world.Config{
						Provider:        &countingProvider{countingStore: p},
						Generator:       gen,
						RandomTickSpeed: -1,
						Synchronous:     synchronous,
						SaveChunks:      mode,
					}.New()
				}
				// expect checks that every column was stored once since the
				// last call if all is set, and that none was otherwise.
				expect := func(step string, all bool) {
					t.Helper()
					got := p.takeStores()
					want := 0
					if all {
						want = 1
					}
					for _, pos := range columns {
						if got[pos] != want {
							t.Errorf("%s: column %v stored %d times, want %d", step, pos, got[pos], want)
						}
					}
					if len(got) > n {
						t.Errorf("%s: stored columns not loaded: %v", step, got)
					}
				}
				every := mode == world.ChunkSavingAll

				w := newWorld()
				t.Cleanup(func() { _ = w.Close() })
				<-w.Exec(load)
				w.Save()
				expect("first save of the columns generated", true)
				w.Save()
				expect("second save with nothing changed", every)
				<-w.Exec(func(tx *world.Tx) {
					if got, _, _ := w.CollectGarbage(tx); got != n {
						t.Errorf("unloaded %d chunks, want %d", got, n)
					}
				})
				w.Save()
				expect("unloading the columns saved", every)

				// Read back from the provider and left unchanged.
				<-w.Exec(load)
				w.Save()
				expect("saving the columns read back", every)
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}
				expect("closing the world", every)

				// Generated and closed without a save before: closing stores
				// them in either mode.
				p.columns = make(map[world.ChunkPos]*chunk.Column)
				w = newWorld()
				<-w.Exec(load)
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}
				expect("closing a world with the columns generated", true)

				// Loaded again in a new world, a column holds what was
				// generated.
				w = newWorld()
				<-w.Exec(func(tx *world.Tx) {
					load(tx)
					for _, pos := range columns {
						if got := tx.Block(bottom(pos)); got != (block.Bedrock{}) {
							t.Errorf("bottom block of column %v loaded again = %#v, want bedrock", pos, got)
						}
					}
				})
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}
				expect("closing the world loaded again", every)
			})
		}
	}
}

// TestSaveChunksSkipsFailedLoads checks that neither mode stores the empty
// column standing in for one that failed to load, changed or not, when the
// world saves, unloads it or closes, while the column beside it is stored.
func TestSaveChunksSkipsFailedLoads(t *testing.T) {
	for _, mode := range []world.ChunkSaving{world.ChunkSavingModified, world.ChunkSavingAll} {
		for _, synchronous := range []bool{true, false} {
			t.Run(fmt.Sprintf("%v/synchronous=%v", mode, synchronous), func(t *testing.T) {
				t.Parallel()
				failPos, goodPos := world.ChunkPos{0, 0}, world.ChunkPos{4, 0}
				failBlock, goodBlock := cube.Pos{1, 10, 1}, cube.Pos{65, 10, 1}
				store := &columnStore{
					failPos: failPos,
					columns: make(map[world.ChunkPos]*chunk.Column),
					stores:  make(map[world.ChunkPos]int),
				}
				newWorld := func() *world.World {
					return world.Config{
						Provider:        &failingLoadProvider{columnStore: store},
						Generator:       world.NopGenerator{},
						RandomTickSpeed: -1,
						Synchronous:     synchronous,
						SaveChunks:      mode,
					}.New()
				}
				w := newWorld()
				<-w.Exec(func(tx *world.Tx) {
					tx.SetBlock(failBlock, block.Dirt{}, nil)
					tx.SetBlock(goodBlock, block.Dirt{}, nil)
				})
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}
				stored := store.column(failPos)

				store.setFail(true)
				w = newWorld()
				// Read only, then saved and unloaded.
				<-w.Exec(func(tx *world.Tx) {
					_ = tx.Block(failBlock)
					tx.SetBlock(goodBlock, block.Stone{}, nil)
				})
				w.Save()
				<-w.Exec(func(tx *world.Tx) {
					if n, _, _ := w.CollectGarbage(tx); n != 2 {
						t.Errorf("unloaded %d chunks, want 2", n)
					}
					// Loaded again and changed, then left for Close.
					tx.SetBlock(failBlock, block.Stone{}, nil)
					tx.SetBlock(goodBlock, block.Dirt{}, nil)
				})
				w.Save()
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}
				if n := store.storesAt(failPos); n != 0 {
					t.Errorf("column that failed to load was stored %d times", n)
				}
				if store.column(failPos) != stored {
					t.Error("stored column of the position that failed to load was replaced")
				}
				if n := store.storesAt(goodPos); n < 2 {
					t.Errorf("column next to it was stored %d times, want at least 2", n)
				}
			})
		}
	}
}

// panicGenerator generates flat terrain, but panics generating the column at
// pos, which the world is left to hold as air.
type panicGenerator struct {
	generator.Flat
	pos world.ChunkPos
}

func (g panicGenerator) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk, ctx world.GenerationContext) world.GenerationResult {
	if pos == g.pos {
		panic("injected: generator failed")
	}
	return g.Flat.GenerateChunk(pos, c, ctx)
}

// TestSaveChunksSkipsGenerationCutShort checks that neither mode stores the
// air a world holds in place of a column whose generation panicked, so that
// the column is generated again once loaded again, while the column beside it
// is stored.
func TestSaveChunksSkipsGenerationCutShort(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	failPos, goodPos := world.ChunkPos{0, 0}, world.ChunkPos{4, 0}
	gen := panicGenerator{Flat: generator.NewFlat(biome.Plains{}, []world.Block{block.Grass{}, block.Bedrock{}}), pos: failPos}
	for _, mode := range []world.ChunkSaving{world.ChunkSavingModified, world.ChunkSavingAll} {
		for _, synchronous := range []bool{true, false} {
			t.Run(fmt.Sprintf("%v/synchronous=%v", mode, synchronous), func(t *testing.T) {
				t.Parallel()
				p := newCountingStore()
				w := world.Config{
					Provider:        &countingProvider{countingStore: p},
					Generator:       gen,
					RandomTickSpeed: -1,
					Synchronous:     synchronous,
					SaveChunks:      mode,
					Log:             slog.New(slog.DiscardHandler),
				}.New()
				<-w.Exec(func(tx *world.Tx) {
					_ = tx.Block(cube.Pos{1, 0, 1})
					_ = tx.Block(cube.Pos{65, 0, 1})
				})
				w.Save()
				w.Save()
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}
				stores := p.takeStores()
				if n := stores[failPos]; n != 0 {
					t.Errorf("column whose generation panicked was stored %d times", n)
				}
				if stores[goodPos] == 0 {
					t.Error("column generated beside it was not stored")
				}
			})
		}
	}
}

// TestParseChunkSaving checks the values SaveChunks takes.
func TestParseChunkSaving(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want world.ChunkSaving
	}{
		{"", world.ChunkSavingModified}, {"modified", world.ChunkSavingModified}, {" Modified ", world.ChunkSavingModified},
		{"all", world.ChunkSavingAll}, {"ALL", world.ChunkSavingAll},
	} {
		in, want := tc.in, tc.want
		if got, err := world.ParseChunkSaving(in); err != nil || got != want {
			t.Errorf("ParseChunkSaving(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := world.ParseChunkSaving("dirty"); err == nil || err.Error() != `unknown SaveChunks "dirty": use "modified" or "all"` {
		t.Errorf("ParseChunkSaving(\"dirty\"): error %v", err)
	}
}
