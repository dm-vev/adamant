package mcdb

import (
	"sync"
	"testing"

	_ "github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// TestLoadColumnsConcurrently checks that columns are read back right from
// many goroutines at once, as the world reads the columns its loaders are
// about to ask for ahead of its owner. It is run under the race detector in
// the checks of the repository.
func TestLoadColumnsConcurrently(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	stone, ok := world.DefaultBlockRegistry.StateToRuntimeID("minecraft:stone", map[string]any{})
	if !ok {
		t.Fatal("no stone state")
	}
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if !db.LoadsConcurrently() {
		t.Fatal("the database does not say it loads concurrently")
	}
	const columns = 32
	for i := range columns {
		c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
		c.SetBlock(uint8(i%16), int16(60+i), 3, 0, stone)
		if err := db.StoreColumn(world.ChunkPos{int32(i), -int32(i)}, world.Overworld, &chunk.Column{Chunk: c}); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for round := range 4 {
				for i := range columns {
					i := (i + g*5 + round) % columns
					col, err := db.LoadColumn(world.ChunkPos{int32(i), -int32(i)}, world.Overworld)
					if err != nil {
						t.Errorf("column %d: %v", i, err)
						return
					}
					if got := col.Chunk.Block(uint8(i%16), int16(60+i), 3, 0); got != stone {
						t.Errorf("column %d holds block %d where %d was stored", i, got, stone)
						return
					}
				}
			}
		}()
	}
	wg.Wait()
}
