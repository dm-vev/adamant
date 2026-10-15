package world_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
)

// TestUnloadedChunkReloadsWithItsChanges checks that chunks unloaded and
// loaded again keep the blocks set in them, whether the background saver
// already wrote them or not, and that they survive closing the world.
func TestUnloadedChunkReloadsWithItsChanges(t *testing.T) {
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
