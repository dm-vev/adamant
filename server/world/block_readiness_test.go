package world_test

import (
	"context"
	"math/rand/v2"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

func TestBlockTicksWaitForFullNeighbourhoodWithoutLosingScheduledTicks(t *testing.T) {
	b, registry := newReadinessTickRegistry()
	g := &blockReadinessGenerator{held: world.ChunkPos{1, 1}, started: make(chan struct{}), release: make(chan struct{}), block: b, registry: registry}
	w := world.Config{Blocks: registry, Generator: g, GeneratorWorkers: 1, RandomTickSpeed: 1}.New()
	w.Freeze()
	t.Cleanup(func() { g.releaseGeneration(); _ = w.Close() })
	loader := world.NewLoader(1, w, world.NopViewer{})
	t.Cleanup(func() { runLavaWorldTask(t, w, func(tx *world.Tx) { loader.Close(tx) }) })
	runLavaWorldTask(t, w, func(tx *world.Tx) { loader.Load(tx, 9) })
	select {
	case <-g.started:
	case <-time.After(5 * time.Second):
		t.Fatal("neighbour generation did not start")
	}
	runLavaWorldTask(t, w, func(tx *world.Tx) {
		tx.ScheduleBlockUpdate(cube.Pos{8, 64, 8}, b, time.Second/20)
	})
	stepReadinessWorld(t, w)
	if random, scheduled := b.random.Load(), b.scheduled.Load(); random != 0 || scheduled != 0 {
		t.Fatalf("block simulation ran before the FULL neighbourhood: random=%d scheduled=%d", random, scheduled)
	}
	g.releaseGeneration()
	loadReadinessNeighbourhood(t, w)
	stepReadinessWorld(t, w)
	if random, scheduled := b.random.Load(), b.scheduled.Load(); random != 1 || scheduled != 1 {
		t.Fatalf("eligible simulation did not resume exactly once: random=%d scheduled=%d", random, scheduled)
	}
	stepReadinessWorld(t, w)
	if random, scheduled := b.random.Load(), b.scheduled.Load(); random != 2 || scheduled != 1 {
		t.Fatalf("scheduled tick was duplicated or random ticking stopped: random=%d scheduled=%d", random, scheduled)
	}
}

func TestRestoredForcedTicketDoesNotSuspendOwnerAndResumesTicks(t *testing.T) {
	data := &world.WorldDataMemory{}
	b, registry := newReadinessTickRegistry()
	initial := world.Config{Synchronous: true, Blocks: registry, Provider: world.NopProvider{WorldData: data}}.New()
	runLavaWorldTask(t, initial, func(tx *world.Tx) {
		if err := tx.ForceChunk(world.ChunkPos{}, true); err != nil {
			t.Error(err)
		}
	})
	if err := initial.Close(); err != nil {
		t.Fatal(err)
	}
	g := &blockReadinessGenerator{started: make(chan struct{}), release: make(chan struct{}), block: b, registry: registry, generatedTick: true}
	created := make(chan *world.World, 1)
	go func() {
		created <- world.Config{Blocks: registry, Provider: world.NopProvider{WorldData: data}, Generator: g, GeneratorWorkers: 1, RandomTickSpeed: 1}.New()
	}()
	select {
	case <-g.started:
	case <-time.After(5 * time.Second):
		g.releaseGeneration()
		_ = (<-created).Close()
		t.Fatal("restored ticket did not request terrain")
	}
	var w *world.World
	select {
	case w = <-created:
	case <-time.After(time.Second):
		g.releaseGeneration()
		_ = (<-created).Close()
		t.Fatal("restored ticket suspended world construction before readiness")
	}
	w.Freeze()
	t.Cleanup(func() { g.releaseGeneration(); _ = w.Close() })
	progress := w.Do(func(*world.Tx) { w.SetTime(6000) })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	err := progress.Wait(ctx)
	cancel()
	observed := w.Time()
	g.releaseGeneration()
	if err != nil {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		_ = progress.Wait(ctx)
		cancel()
		t.Fatalf("restored ticket suspended owner before readiness: %v", err)
	}
	if observed < 6000 {
		t.Fatalf("owner did not process queued command: time=%d", observed)
	}
	loadReadinessNeighbourhood(t, w)
	stepReadinessWorld(t, w)
	stepReadinessWorld(t, w)
	if random, scheduled := b.random.Load(), b.scheduled.Load(); random != 2 || scheduled != 1 {
		t.Fatalf("forced chunk did not promote generation and tick: random=%d scheduled=%d", random, scheduled)
	}
	runLavaWorldTask(t, w, func(tx *world.Tx) {
		w.CollectGarbage(tx)
		if !tx.ChunkLoaded(world.ChunkPos{}) || !tx.ChunkLoaded(world.ChunkPos{1, 1}) {
			t.Error("garbage collection discarded the forced ticket's FULL neighbourhood")
		}
		if err := tx.ClearForcedChunks(); err != nil {
			t.Error(err)
		}
		w.CollectGarbage(tx)
		if loaded, _ := tx.ChunkState(world.ChunkPos{}); loaded {
			t.Error("cleared forced ticket retained its centre")
		}
		if loaded, _ := tx.ChunkState(world.ChunkPos{1, 1}); loaded {
			t.Error("cleared forced ticket leaked its neighbourhood")
		}
	})
}

func stepReadinessWorld(t *testing.T, w *world.World) {
	t.Helper()
	if err := w.StepTicks(1); err != nil {
		t.Fatal(err)
	}
	w.AdvanceTick()
}

func loadReadinessNeighbourhood(t *testing.T, w *world.World) {
	t.Helper()
	runLavaWorldTask(t, w, func(tx *world.Tx) {
		for x := -1; x <= 1; x++ {
			for z := -1; z <= 1; z++ {
				_ = tx.Block(cube.Pos{x * 16, 64, z * 16})
			}
		}
	})
}

func newReadinessTickRegistry() (readinessTickBlock, world.BlockRegistry) {
	b := readinessTickBlock{random: &atomic.Int32{}, scheduled: &atomic.Int32{}}
	registry := world.NewBlockRegistry()
	name, properties := b.EncodeBlock()
	registry.RegisterBlockState(world.BlockState{Name: name, Properties: properties})
	registry.RegisterBlock(b)
	return b, registry
}

type readinessTickBlock struct {
	block.Stone
	random, scheduled *atomic.Int32
}

func (readinessTickBlock) EncodeBlock() (string, map[string]any)           { return "test:readiness_tick", nil }
func (readinessTickBlock) Hash() (uint64, uint64)                          { return 1 << 58, 0 }
func (b readinessTickBlock) RandomTick(cube.Pos, *world.Tx, *rand.Rand)    { b.random.Add(1) }
func (b readinessTickBlock) ScheduledTick(cube.Pos, *world.Tx, *rand.Rand) { b.scheduled.Add(1) }

type blockReadinessGenerator struct {
	held             world.ChunkPos
	started, release chan struct{}
	block            readinessTickBlock
	registry         world.BlockRegistry
	generatedTick    bool
}

func (g *blockReadinessGenerator) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk, _ world.GenerationContext) world.GenerationResult {
	if pos == g.held {
		close(g.started)
		<-g.release
	}
	if pos != (world.ChunkPos{}) {
		return world.GenerationResult{}
	}
	rid := g.registry.BlockRuntimeID(g.block)
	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			for y := int16(64); y < 80; y++ {
				c.SetBlock(x, y, z, 0, rid)
			}
		}
	}
	if g.generatedTick {
		return world.GenerationResult{ScheduledTicks: []chunk.ScheduledTick{{Pos: cube.Pos{8, 64, 8}, Type: "test:readiness_tick"}}}
	}
	return world.GenerationResult{}
}

func (*blockReadinessGenerator) DefaultSpawn(world.Dimension) cube.Pos { return cube.Pos{} }
func (g *blockReadinessGenerator) releaseGeneration() {
	select {
	case <-g.release:
	default:
		close(g.release)
	}
}
