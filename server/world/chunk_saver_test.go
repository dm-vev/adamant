package world_test

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
)

// TestUnloadedChunkReloadsWithItsChanges checks that chunks unloaded and
// loaded again keep the blocks set in them, whether the background saver
// already wrote them or not, and that they survive closing the world.
func TestUnloadedChunkReloadsWithItsChanges(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := (mcdb.Config{}).Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	w := world.Config{Provider: db, Generator: world.NopGenerator{}, RandomTickSpeed: -1}.New()
	positions := make([]cube.Pos, 0, 64)
	for i := range 64 {
		positions = append(positions, cube.Pos{i * 16, 10, 0})
	}
	for round := range 3 {
		<-w.Exec(func(tx *world.Tx) {
			for _, pos := range positions {
				if round > 0 {
					if got := tx.Block(pos); got != (block.Stone{}) {
						t.Errorf("round %d: block at %v = %#v after reloading, want stone", round, pos, got)
					}
				}
				tx.SetBlock(pos, block.Stone{}, nil)
			}
			// Nothing views these chunks: collecting them saves and unloads
			// them, and the next round loads them again at once.
			w.CollectGarbage(tx)
		})
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = (mcdb.Config{}).Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	w = world.Config{Provider: db, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		for _, pos := range positions {
			if got := tx.Block(pos); got != (block.Stone{}) {
				t.Errorf("block at %v = %#v after reopening the world, want stone", pos, got)
			}
		}
	})
}

// TestUnloadedChunksAreStagedUntilTheNextSave checks that the columns a
// world unloads are not stored until the world is next saved, that a column
// loaded again before then holds its changes and is stored by the save as
// it is then, and that columns staged beyond the StagingLimit ask for a
// save, once.
func TestUnloadedChunksAreStagedUntilTheNextSave(t *testing.T) {
	t.Parallel()
	p := newCountingStore()
	full := make(chan int, 4)
	newWorld := func() *world.World {
		return world.Config{
			Provider:        &countingProvider{countingStore: p},
			Generator:       world.NopGenerator{},
			RandomTickSpeed: -1,
			StagingLimit:    1,
			StagingFull:     func(_ *world.World, bytes int) { full <- bytes },
		}.New()
	}
	w := newWorld()
	a, b := cube.Pos{1, 10, 1}, cube.Pos{33, 10, 1}
	posA, posB := world.ChunkPos{0, 0}, world.ChunkPos{2, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(a, block.Stone{}, nil)
		tx.SetBlock(b, block.Stone{}, nil)
		if n, _, _ := w.CollectGarbage(tx); n != 2 {
			t.Errorf("unloaded %d chunks, want 2", n)
		}
	})
	select {
	case bytes := <-full:
		if bytes <= 1 {
			t.Errorf("StagingFull was called with %d bytes staged, over a limit of 1", bytes)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("StagingFull was not called with the staged columns over the limit")
	}
	if got := p.takeStores(); len(got) != 0 {
		t.Errorf("unloading stored %v before the next save", got)
	}

	<-w.Exec(func(tx *world.Tx) {
		if got := tx.Block(a); got != (block.Stone{}) {
			t.Errorf("block at %v = %#v after loading its staged chunk, want stone", a, got)
		}
		tx.SetBlock(a, block.Dirt{}, nil)
	})
	if got := p.takeStores(); len(got) != 0 {
		t.Errorf("loading a staged chunk stored %v", got)
	}
	w.Save()
	if got := p.takeStores(); len(got) != 2 || got[posA] != 1 || got[posB] != 1 {
		t.Errorf("the save after unloading stored %v, want %v and %v once", got, posA, posB)
	}
	select {
	case bytes := <-full:
		t.Errorf("StagingFull was called again, with %d bytes", bytes)
	default:
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	w = newWorld()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		for pos, want := range map[cube.Pos]world.Block{a: block.Dirt{}, b: block.Stone{}} {
			if got := tx.Block(pos); got != want {
				t.Errorf("block at %v after loading the world again = %#v, want %#v", pos, got, want)
			}
		}
	})
}
