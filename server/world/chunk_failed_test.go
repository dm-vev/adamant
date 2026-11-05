package world_test

import (
	"errors"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// failingColumns is a provider that fails to load the columns it is told to.
type failingColumns struct {
	world.NopProvider
	fail map[world.ChunkPos]bool
}

func (f failingColumns) LoadColumn(pos world.ChunkPos, dim world.Dimension) (*chunk.Column, error) {
	if f.fail[pos] {
		return nil, errors.New("disk on fire")
	}
	return f.NopProvider.LoadColumn(pos, dim)
}

// TestChunkFailed checks that ChunkFailed is true for the stand-in of a
// chunk that failed to load, false for the chunks that loaded and for those
// not loaded, and that asking loads nothing.
func TestChunkFailed(t *testing.T) {
	w := world.Config{Synchronous: true, Provider: failingColumns{NopProvider: world.NopProvider{WorldData: &world.WorldDataMemory{}}, fail: map[world.ChunkPos]bool{{1, 0}: true}}}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		if tx.ChunkFailed(world.ChunkPos{1, 0}) {
			t.Fatal("a chunk not loaded is a failed one")
		}
		if _, tracked := tx.ChunkState(world.ChunkPos{1, 0}); tracked {
			t.Fatal("ChunkFailed loaded the chunk")
		}
		tx.Block(cube.Pos{0, 64, 0})
		tx.Block(cube.Pos{16, 64, 0})
		if tx.ChunkFailed(world.ChunkPos{0, 0}) {
			t.Error("a chunk that loaded is a failed one")
		}
		if !tx.ChunkFailed(world.ChunkPos{1, 0}) {
			t.Error("the stand-in of a chunk that failed to load is not a failed one")
		}
	})
}
