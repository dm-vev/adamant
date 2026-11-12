package world

import (
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

// abandonTestGenerator holds every chunk it generates until gate is closed, and
// records the chunks it started and generated.
type abandonTestGenerator struct {
	gate    chan struct{}
	started chan ChunkPos

	mu        sync.Mutex
	generated map[ChunkPos]int
}

func newAbandonTestGenerator() *abandonTestGenerator {
	return &abandonTestGenerator{gate: make(chan struct{}), started: make(chan ChunkPos, 64), generated: map[ChunkPos]int{}}
}

func (*abandonTestGenerator) DefaultSpawn(Dimension) cube.Pos { return cube.Pos{8, 70, 8} }

func (g *abandonTestGenerator) GenerateChunk(pos ChunkPos, _ *chunk.Chunk, _ GenerationContext) GenerationResult {
	g.started <- pos
	<-g.gate
	g.mu.Lock()
	g.generated[pos]++
	g.mu.Unlock()
	return GenerationResult{}
}

func (g *abandonTestGenerator) generatedAt(pos ChunkPos) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.generated[pos]
}

// abandonTestProvider records the positions of the columns stored.
type abandonTestProvider struct {
	NopProvider
	mu     sync.Mutex
	stored map[ChunkPos]int
}

func (p *abandonTestProvider) StoreColumn(pos ChunkPos, _ Dimension, _ *chunk.Column) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stored[pos]++
	return nil
}

func (p *abandonTestProvider) storedAt(pos ChunkPos) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stored[pos]
}

// abandonTestViewer records the chunks it was shown.
type abandonTestViewer struct {
	NopViewer
	mu     sync.Mutex
	viewed map[ChunkPos]bool
}

func (v *abandonTestViewer) ViewChunk(pos ChunkPos, _ Dimension, _ *Column) {
	v.mu.Lock()
	v.viewed[pos] = true
	v.mu.Unlock()
}

func (v *abandonTestViewer) count() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.viewed)
}

// TestLoaderReleasesQueuedColumnsItLeft checks that the columns a loader
// asked for and left while they waited for generation are released: they
// are neither kept loaded nor generated later nor saved, and are generated
// once the loader comes back.
func TestLoaderReleasesQueuedColumnsItLeft(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)
	g := newAbandonTestGenerator()
	provider := &abandonTestProvider{stored: map[ChunkPos]int{}}
	w := Config{Generator: g, Provider: provider, GeneratorWorkers: 1, ChunkUnloadInterval: time.Hour}.New()
	gateOpen := false
	t.Cleanup(func() {
		if !gateOpen {
			close(g.gate)
		}
		_ = w.Close()
	})
	viewer := &abandonTestViewer{viewed: map[ChunkPos]bool{}}
	loader := NewLoader(1, w, viewer)
	positions := loaderOffsets(1, false)

	mustDo := func(f func(tx *Tx)) {
		t.Helper()
		if err := w.Do(f).Wait(ctx); err != nil {
			t.Fatal(err)
		}
	}
	mustDo(func(tx *Tx) { loader.Load(tx, len(positions)) })
	var first ChunkPos
	select {
	case first = <-g.started:
	case <-ctx.Done():
		t.Fatal("generation did not start")
	}
	var queued []ChunkPos
	for _, pos := range positions {
		if pos != first {
			queued = append(queued, pos)
		}
	}

	// The loader leaves while the worker holds the first column and the
	// others wait in the queue.
	mustDo(func(tx *Tx) { loader.Move(tx, mgl64.Vec3{100 * 16, 70, 8}) })
	mustDo(func(tx *Tx) {
		for _, pos := range queued {
			if _, ok := w.chunks[pos]; ok {
				t.Errorf("column %v the loader left is still loaded", pos)
			}
			if n := len(w.chunkRequests[pos]); n != 0 {
				t.Errorf("column %v the loader left has %d requests", pos, n)
			}
		}
		if _, ok := w.chunks[first]; !ok {
			t.Errorf("column %v a worker started was unloaded before it was ready", first)
		}
	})
	loader.mu.RLock()
	pending := len(loader.pending)
	loader.mu.RUnlock()
	if pending != 0 {
		t.Fatalf("loader still awaits %d columns it left", pending)
	}

	close(g.gate)
	gateOpen = true
	// Once the worker is done, nothing else is generated.
	mustDo(func(tx *Tx) { w.waitColumnReady(first, w.chunks[first]) })
	mustDo(func(tx *Tx) {
		if n := w.generatorQueue.length.Load(); n != 0 {
			// The queue drops the abandoned tasks when a worker next looks.
			w.generatorQueue.mu.Lock()
			w.generatorQueue.dropAbandoned()
			n = int64(len(w.generatorQueue.tasks))
			w.generatorQueue.mu.Unlock()
			if n != 0 {
				t.Errorf("%d generation tasks left for columns nobody wants", n)
			}
		}
	})
	select {
	case pos := <-g.started:
		t.Fatalf("column %v was generated after the loader left it", pos)
	default:
	}
	w.Save()
	for _, pos := range queued {
		if n := g.generatedAt(pos); n != 0 {
			t.Errorf("column %v the loader left was generated %d times", pos, n)
		}
		if n := provider.storedAt(pos); n != 0 {
			t.Errorf("column %v the loader left was saved %d times", pos, n)
		}
	}

	// The loader comes back: every column is generated and shown.
	mustDo(func(tx *Tx) {
		loader.Move(tx, mgl64.Vec3{8, 70, 8})
		loader.Load(tx, len(positions))
	})
	for range queued {
		select {
		case <-g.started:
		case <-ctx.Done():
			t.Fatal("columns the loader came back to were not generated")
		}
	}
	for viewer.count() < len(positions) {
		mustDo(func(tx *Tx) { loader.Load(tx, len(positions)) })
		time.Sleep(time.Millisecond)
		if ctx.Err() != nil {
			t.Fatalf("loader that came back was shown %d of %d columns", viewer.count(), len(positions))
		}
	}
	for _, pos := range queued {
		if n := g.generatedAt(pos); n != 1 {
			t.Errorf("column %v was generated %d times, want 1", pos, n)
		}
	}
}

// TestCloseLoaderReleasesQueuedColumns checks that a loader closing while
// its columns wait for generation releases them.
func TestCloseLoaderReleasesQueuedColumns(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)
	g := newAbandonTestGenerator()
	w := Config{Generator: g, GeneratorWorkers: 1}.New()
	t.Cleanup(func() {
		close(g.gate)
		_ = w.Close()
	})
	loader := NewLoader(1, w, NopViewer{})
	if err := w.Do(func(tx *Tx) { loader.Load(tx, 9) }).Wait(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-g.started:
	case <-ctx.Done():
		t.Fatal("generation did not start")
	}
	if err := w.Do(func(tx *Tx) { loader.Close(tx) }).Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if n := w.LoadedChunkCount(); n != 1 {
		t.Fatalf("%d columns loaded after the loader closed, want only the one generating", n)
	}
}

// TestCollectGarbageAbandonsUnwantedQueuedColumn checks that a column queued
// for generation that nothing waits for is unloaded, and its task skipped.
func TestCollectGarbageAbandonsUnwantedQueuedColumn(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)
	g := newAbandonTestGenerator()
	w := Config{Generator: g, GeneratorWorkers: 1, ChunkUnloadInterval: time.Hour}.New()
	t.Cleanup(func() {
		close(g.gate)
		_ = w.Close()
	})
	var queued *Column
	err := w.Do(func(tx *Tx) {
		if _, err := w.loadChunk(ChunkPos{0, 0}); err != nil {
			t.Error(err)
		}
		<-g.started
		c, err := w.loadChunk(ChunkPos{5, 5})
		if err != nil {
			t.Error(err)
		}
		queued = c
		if c.Chunk != nil {
			t.Error("a column waiting for generation holds a chunk")
		}
		if collected, _, _ := w.CollectGarbage(tx); collected != 1 {
			t.Errorf("collected %d columns, want the queued one", collected)
		}
		if _, ok := w.chunks[ChunkPos{5, 5}]; ok {
			t.Error("queued column nothing wants is still loaded")
		}
		if _, ok := w.chunks[ChunkPos{0, 0}]; !ok {
			t.Error("column a worker started was unloaded")
		}
	}).Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !queued.abandoned() || !queued.Ready() {
		t.Fatal("unloaded queued column was not abandoned")
	}
}

// TestGenerationQueueDropsAbandonedTasks checks that workers never take the
// task of an abandoned column.
func TestGenerationQueueDropsAbandonedTasks(t *testing.T) {
	t.Parallel()
	q := newGenerationQueue()
	cols := make([]*Column, 4)
	for i := range cols {
		cols[i] = newColumn(nil)
		cols[i].generationState.Store(generationQueued)
		q.push(generationTask{pos: ChunkPos{int32(i), 0}, col: cols[i]})
	}
	cols[0].abandonGeneration()
	cols[2].abandonGeneration()
	for _, want := range []ChunkPos{{1, 0}, {3, 0}} {
		if task, _ := q.pop(); task.pos != want {
			t.Fatalf("task = %v, want %v", task.pos, want)
		}
	}
	if n := q.length.Load(); n != 0 {
		t.Fatalf("%d tasks left, want 0", n)
	}
	if !cols[1].startGeneration() || cols[1].abandonGeneration() {
		t.Fatal("abandoned a column a worker started")
	}
	if cols[0].startGeneration() {
		t.Fatal("started an abandoned column")
	}
}

// TestGenerationQueuePrioritizesLiveTask checks that the chunk the owner
// waits for is the column loaded at its position, not the abandoned one
// whose task still waits beside it.
func TestGenerationQueuePrioritizesLiveTask(t *testing.T) {
	t.Parallel()
	q := newGenerationQueue()
	old, live := newQueuedColumn(), newQueuedColumn()
	q.push(generationTask{pos: ChunkPos{0, 0}})
	q.push(generationTask{pos: ChunkPos{5, 5}, col: old})
	q.push(generationTask{pos: ChunkPos{5, 5}, col: live})
	old.abandonGeneration()
	q.prioritize(ChunkPos{5, 5})
	if task, _ := q.pop(); task.col != live {
		t.Fatalf("task = %v of %p, want the live column %p", task.pos, task.col, live)
	}
}
