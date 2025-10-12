package world_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/pmgen"
	_ "unsafe"
)

// TestPMGenMassChunkGeneration ensures that the pmgen generator can populate a large batch of chunks
// without stalling the world transaction loop. The test stresses the async generation path by queuing
// hundreds of Exec transactions that each require full chunk generation.
func TestPMGenMassChunkGeneration(t *testing.T) {
	t.Parallel()

	world_finaliseBlockRegistry()

	gen := pmgen.NewOverworld(42)
	conf := world.Config{
		Generator:    gen,
		Provider:     world.NopProvider{},
		SaveInterval: -1,
	}
	w := conf.New()
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Errorf("world close: %v", err)
		}
	})
	gen.BindWorld(w)

	radius := int32(12)
	positions := make([]world.ChunkPos, 0, (radius*2+1)*(radius*2+1))
	for x := -radius; x <= radius; x++ {
		for z := -radius; z <= radius; z++ {
			positions = append(positions, world.ChunkPos{x, z})
		}
	}

	errCh := make(chan error, len(positions))
	var wg sync.WaitGroup
	for _, pos := range positions {
		pos := pos
		wg.Add(1)
		go func() {
			defer wg.Done()

			blockPos := cube.Pos{int(pos[0] << 4), 64, int(pos[1] << 4)}
			done := w.Exec(func(tx *world.Tx) {
				tx.Block(blockPos)
			})

			timer := time.NewTimer(10 * time.Second)
			defer timer.Stop()

			select {
			case <-done:
				return
			case <-timer.C:
				errCh <- fmt.Errorf("timeout generating chunk at %v", pos)
			}
		}()
	}

	finished := make(chan struct{})
	go func() {
		wg.Wait()
		close(finished)
	}()

	select {
	case err := <-errCh:
		t.Fatalf("mass chunk generation failed: %v", err)
	case <-finished:
	case <-time.After(30 * time.Second):
		t.Fatal("mass chunk generation timed out")
	}
}

//go:linkname world_finaliseBlockRegistry github.com/df-mc/dragonfly/server/world.finaliseBlockRegistry
func world_finaliseBlockRegistry()
