package world

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// releasingGenerator is a generator that blocks the chunk at block while it
// is set, and counts the times its caches were released.
type releasingGenerator struct {
	released atomic.Int64
	block    atomic.Pointer[chan struct{}]
	started  chan struct{}
}

func (g *releasingGenerator) GenerateChunk(pos ChunkPos, _ *chunk.Chunk, _ GenerationContext) GenerationResult {
	if hold := g.block.Load(); hold != nil && pos == (ChunkPos{5, 5}) {
		close(g.started)
		<-*hold
	}
	return GenerationResult{}
}

func (*releasingGenerator) DefaultSpawn(Dimension) cube.Pos { return cube.Pos{} }

func (g *releasingGenerator) ReleaseGenerationCaches() { g.released.Add(1) }

// TestGeneratorCachesAreReleasedOnceGenerationIdles checks that the world has
// its generator release its caches when no chunk has been generated for the
// time set, not while a chunk is being generated, and again after each time
// generation idled.
func TestGeneratorCachesAreReleasedOnceGenerationIdles(t *testing.T) {
	g := &releasingGenerator{started: make(chan struct{})}
	hold := make(chan struct{})
	g.block.Store(&hold)
	w := Config{Generator: g, GeneratorWorkers: 2}.New()
	defer w.Close()
	w.releaseMu.Lock()
	w.releaseAfter = 30 * time.Millisecond
	w.releaseMu.Unlock()

	// Two chunks: one generated at once, one that is held.
	var loaded []<-chan struct{}
	<-w.Exec(func(tx *Tx) {
		loaded = append(loaded, tx.LoadArea(cube.Pos{0, 0, 0}, cube.Pos{0, 0, 0}))
		loaded = append(loaded, tx.LoadArea(cube.Pos{5 * 16, 0, 5 * 16}, cube.Pos{5 * 16, 0, 5 * 16}))
	})
	<-loaded[0]
	<-g.started
	time.Sleep(150 * time.Millisecond)
	if n := g.released.Load(); n != 0 {
		t.Fatalf("caches released %d times while a chunk was being generated", n)
	}

	close(hold)
	<-loaded[1]
	waitFor(t, 10*time.Second, "the caches to be released once generation idled", func() bool { return g.released.Load() == 1 })
	if got := w.Stats().CachesReleased.Load(); got != 1 {
		t.Errorf("the world counted %d releases, want 1", got)
	}

	// Generation resumes: the caches are released again once it idles.
	g.block.Store(nil)
	<-w.Exec(func(tx *Tx) { loaded[0] = tx.LoadArea(cube.Pos{9 * 16, 0, 9 * 16}, cube.Pos{9 * 16, 0, 9 * 16}) })
	<-loaded[0]
	waitFor(t, 10*time.Second, "the caches to be released again", func() bool { return g.released.Load() == 2 })
}
