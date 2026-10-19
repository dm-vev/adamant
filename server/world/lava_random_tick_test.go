package world_test

import (
	"context"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

func TestLavaRandomTickDoesNotWaitForGeneratingNeighbour(t *testing.T) {
	generator := &lavaReadinessGenerator{started: make(chan struct{}), release: make(chan struct{})}
	w := world.Config{Generator: generator, GeneratorWorkers: 1, RandomTickSpeed: -1}.New()
	t.Cleanup(func() {
		generator.releaseGeneration()
		_ = w.Close()
	})

	loader := world.NewLoader(1, w, world.NopViewer{})
	t.Cleanup(func() { runLavaWorldTask(t, w, func(tx *world.Tx) { loader.Close(tx) }) })
	runLavaWorldTask(t, w, func(tx *world.Tx) { loader.Load(tx, 4) })

	select {
	case <-generator.started:
	case <-time.After(5 * time.Second):
		t.Fatal("neighbor generation did not start")
	}

	origin := cube.Pos{15, 64, 8}
	seed := lavaEdgeRandomSeed()
	task := w.Do(func(tx *world.Tx) {
		if loaded, ready := tx.ChunkState(world.ChunkPos{}); !loaded || !ready {
			t.Errorf("lava source column state = (%v, %v), want ready", loaded, ready)
			return
		}
		block.Lava{Depth: 8}.RandomTick(origin, tx, rand.New(rand.NewPCG(uint64(seed), 1)))
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	err := task.Wait(ctx)
	cancel()
	if err != nil {
		generator.releaseGeneration()
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		_ = task.Wait(ctx)
		cancel()
		t.Fatal("lava random tick waited for the generating neighbor column")
	}

	generator.releaseGeneration()
	var ready <-chan struct{}
	runLavaWorldTask(t, w, func(tx *world.Tx) {
		var ok bool
		ready, ok = tx.ChunkReadySignal(world.ChunkPos{1, 0})
		if !ok {
			t.Error("neighbor column was not tracked")
		}
	})
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("neighbor column did not become ready after generation was released")
	}
	var firePlaced bool
	var candidate cube.Pos
	runLavaWorldTask(t, w, func(tx *world.Tx) {
		candidate = lavaEdgeCandidate(seed, origin)
		_ = tx.Block(candidate)
		block.Lava{Depth: 8}.RandomTick(origin, tx, rand.New(rand.NewPCG(uint64(seed), 1)))
		_, firePlaced = tx.Block(candidate).(block.Fire)
	})
	if !firePlaced {
		t.Fatalf("lava random tick did not resume fire spread at ready candidate %v", candidate)
	}
}

func runLavaWorldTask(t *testing.T, w *world.World, f func(*world.Tx)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.Do(f).Wait(ctx); err != nil {
		t.Fatalf("world task did not complete: %v", err)
	}
}

func lavaEdgeRandomSeed() int64 {
	for seed := int64(0); ; seed++ {
		r := rand.New(rand.NewPCG(uint64(seed), 1))
		if r.IntN(3) == 0 {
			continue
		}
		if r.IntN(3)-1 == 1 && r.IntN(3)-1 == 0 {
			return seed
		}
	}
}

func lavaEdgeCandidate(seed int64, origin cube.Pos) cube.Pos {
	r := rand.New(rand.NewPCG(uint64(seed), 1))
	i := r.IntN(3)
	if i == 0 {
		panic("seed does not select upper lava spread")
	}
	return origin.Add(cube.Pos{r.IntN(3) - 1, 1, r.IntN(3) - 1})
}

type lavaReadinessGenerator struct {
	started chan struct{}
	release chan struct{}
}

func (g *lavaReadinessGenerator) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk, _ world.GenerationContext) world.GenerationResult {
	switch pos {
	case world.ChunkPos{}:
		c.SetBlock(15, 64, 8, 0, world.BlockRuntimeID(block.Lava{Depth: 8}))
	case world.ChunkPos{1, 0}:
		close(g.started)
		<-g.release
		c.SetBlock(0, 64, 8, 0, world.BlockRuntimeID(block.Log{Wood: block.OakWood()}))
	}
	return world.GenerationResult{}
}

func (g *lavaReadinessGenerator) DefaultSpawn(world.Dimension) cube.Pos { return cube.Pos{} }

func (g *lavaReadinessGenerator) releaseGeneration() {
	select {
	case <-g.release:
	default:
		close(g.release)
	}
}
