package world

import (
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

// TestEntityTickingStopsWhenANeighbourCloses checks a chunk remembered as
// entity ticking stops being so once a chunk within two of it closes.
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
	})
}
