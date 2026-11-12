package world

import (
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/internal/faultcheck"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

// panickingStoreProvider is a provider counting by position the columns it
// is asked to store, whose StoreColumn panics for the column at bad.
type panickingStoreProvider struct {
	NopProvider
	bad ChunkPos

	mu       sync.Mutex
	attempts map[ChunkPos]int
}

func newPanickingStoreProvider(bad ChunkPos) *panickingStoreProvider {
	return &panickingStoreProvider{bad: bad, attempts: make(map[ChunkPos]int)}
}

func (p *panickingStoreProvider) StoreColumn(pos ChunkPos, _ Dimension, _ *chunk.Column) error {
	p.mu.Lock()
	p.attempts[pos]++
	p.mu.Unlock()
	if pos == p.bad {
		panic(faultcheck.Expected{Why: "test: store column"})
	}
	return nil
}

func (p *panickingStoreProvider) storesAt(pos ChunkPos) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.attempts[pos]
}

// saveWithin saves w, ending the process as within does if saving does not
// end after d.
func saveWithin(t *testing.T, w *World, d time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Save()
	}()
	within(t, d, done, "saving")
}

func TestSaverPanicKeepsWriting(t *testing.T) {
	isolated(t, func(t *testing.T) {
		var sink faultSink
		rec := newFaultRecorder()
		bad, good := ChunkPos{0, 0}, ChunkPos{4, 0}
		prov := newPanickingStoreProvider(bad)
		other := cancelBatchTestBlock{name: "test:other", id: 71, tick: func(cube.Pos, *Tx) {}}
		w := sink.configure(Config{Provider: prov, Generator: NopGenerator{}, Blocks: testBlockRegistry(other), RandomTickSpeed: -1}, rec).New()
		defer func() { _ = w.Close() }()
		change := func() {
			within(t, 10*time.Second, w.Exec(func(tx *Tx) {
				tx.SetBlock(cube.Pos{1, 10, 1}, other, nil)
				tx.SetBlock(cube.Pos{65, 10, 1}, other, nil)
			}), "changing the columns")
		}

		// The write of the column panics: the saver goes on, and saving
		// ends.
		change()
		saveWithin(t, w, 10*time.Second)
		if prov.storesAt(bad) != 1 || prov.storesAt(good) != 1 {
			t.Fatalf("stores: panicking column %d, other %d, want 1 each", prov.storesAt(bad), prov.storesAt(good))
		}
		faults := sink.all(FaultSave)
		if len(faults) != 1 || faults[0].Type != "chunk writer" || faults[0].Pos != (cube.Pos{}) || faults[0].Value != expectedPanic("test: store column") || !strings.Contains(string(faults[0].Stack), "fault_save_test.go") {
			t.Fatalf("faults = %v, want one of the writer, with the stack of the provider", sink.all(0))
		}
		// The next save writes it once more; it panics again and is
		// dropped.
		saveWithin(t, w, 10*time.Second)
		if n := prov.storesAt(bad); n != saveAttempts {
			t.Fatalf("the panicking column was written %d times, want %d", n, saveAttempts)
		}
		saveWithin(t, w, 10*time.Second)
		if n := prov.storesAt(bad); n != saveAttempts || prov.storesAt(good) != 1 {
			t.Fatalf("stores after it was dropped: panicking column %d, other %d, want %d and 1", n, prov.storesAt(good), saveAttempts)
		}
		// The world data of a NopProvider fails to save each time: only
		// the lines about columns count.
		var lines []string
		for _, r := range rec.errors() {
			if r.Message == "save panicked" || strings.HasPrefix(r.Message, "save chunk") {
				lines = append(lines, r.Message)
			}
		}
		if len(lines) != 2 || lines[0] != "save panicked" || !strings.HasPrefix(lines[1], "save chunk: dropped") {
			t.Fatalf("Error lines about columns = %q, want the fault and the drop", lines)
		}
		// Changed again, it is saved again.
		change()
		saveWithin(t, w, 10*time.Second)
		if n := prov.storesAt(bad); n != saveAttempts+1 {
			t.Fatalf("the column changed again was written %d times in all, want %d", n, saveAttempts+1)
		}
	})
}

// panicGenerator generates air, and panics the first time it generates the
// column at bad.
type panicGenerator struct {
	bad ChunkPos

	mu    sync.Mutex
	calls int
}

func (g *panicGenerator) GenerateChunk(pos ChunkPos, _ *chunk.Chunk, _ GenerationContext) GenerationResult {
	if pos != g.bad {
		return GenerationResult{}
	}
	g.mu.Lock()
	g.calls++
	first := g.calls == 1
	g.mu.Unlock()
	if first {
		panic(faultcheck.Expected{Why: "test: generation"})
	}
	return GenerationResult{}
}

func (*panicGenerator) DefaultSpawn(Dimension) cube.Pos { return cube.Pos{0, 65, 0} }

func (g *panicGenerator) generated() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}

func TestGenerationPanicIsNeverSaved(t *testing.T) {
	var sink faultSink
	rec := newFaultRecorder()
	bad := ChunkPos{5, 5}
	badPos, goodPos := cube.Pos{81, 10, 81}, cube.Pos{1, 10, 1}
	prov := newPanickingStoreProvider(ChunkPos{-1000, -1000})
	gen := &panicGenerator{bad: bad}
	other := cancelBatchTestBlock{name: "test:other", id: 72, tick: func(cube.Pos, *Tx) {}}
	w := sink.configure(Config{Provider: prov, Generator: gen, Blocks: testBlockRegistry(other), RandomTickSpeed: -1}, rec).New()
	t.Cleanup(func() { _ = w.Close() })

	// The column whose generation panicked is served and can be changed,
	// but saving it, unloading it or closing the world does not store it.
	within(t, 10*time.Second, w.Exec(func(tx *Tx) {
		tx.SetBlock(badPos, other, nil)
		tx.SetBlock(goodPos, other, nil)
		if name, _ := tx.Block(badPos).EncodeBlock(); name != "test:other" {
			t.Errorf("block set in the column whose generation panicked = %v", name)
		}
	}), "changing the columns")
	saveWithin(t, w, 10*time.Second)
	if n := prov.storesAt(bad); n != 0 {
		t.Fatalf("the column whose generation panicked was stored %d times", n)
	}
	if prov.storesAt(ChunkPos{}) == 0 {
		t.Fatal("the other column was not stored")
	}
	faults := sink.all(FaultGeneration)
	if len(faults) != 1 || faults[0].Type != "generator" || faults[0].Pos != (cube.Pos{80, 0, 80}) || !strings.Contains(string(faults[0].Stack), "fault_save_test.go") {
		t.Fatalf("faults = %v, want one of the generator with its stack", sink.all(0))
	}
	rec.mu.Lock()
	warned := slices.ContainsFunc(*rec.records, func(r slog.Record) bool {
		return strings.HasPrefix(r.Message, "Not saving chunk: its generation panicked")
	})
	rec.mu.Unlock()
	if !warned {
		t.Fatal("not saving the column was not logged")
	}

	// Unloaded, it is generated again when it loads, and then saved.
	within(t, 10*time.Second, w.Exec(func(tx *Tx) { w.CollectGarbage(tx) }), "unloading the columns")
	if n := prov.storesAt(bad); n != 0 {
		t.Fatalf("unloading the column whose generation panicked stored it %d times", n)
	}
	within(t, 10*time.Second, w.Exec(func(tx *Tx) {
		if name, _ := tx.Block(badPos).EncodeBlock(); name == "test:other" {
			t.Error("the column reloaded holds the block set before: it was saved")
		}
		tx.SetBlock(badPos, other, nil)
	}), "loading the column again")
	if n := gen.generated(); n != 2 {
		t.Fatalf("the column was generated %d times, want 2", n)
	}
	saveWithin(t, w, 10*time.Second)
	if n := prov.storesAt(bad); n != 1 {
		t.Fatalf("the column generated again was stored %d times, want 1", n)
	}
}

// panicNBTType is an entity type whose NBT panics.
type panicNBTType struct{ testEntityType }

func (panicNBTType) EncodeNBT(*EntityData) map[string]any {
	panic(faultcheck.Expected{Why: "test: entity NBT"})
}

func TestSaveLeavesOutAnEntityWhoseNBTPanics(t *testing.T) {
	var sink faultSink
	rec := newFaultRecorder()
	w := sink.configure(Config{Synchronous: true, RandomTickSpeed: -1}, rec).New()
	t.Cleanup(func() { _ = w.Close() })
	bad := EntitySpawnOpts{Position: mgl64.Vec3{1.5, 4, 1.5}}.New(panicNBTType{}, taskTestEntityConfig{})
	good := EntitySpawnOpts{Position: mgl64.Vec3{2.5, 4, 2.5}}.New(testEntityType{}, taskTestEntityConfig{})
	var col *chunk.Column
	<-w.Exec(func(tx *Tx) {
		tx.AddEntity(bad)
		tx.AddEntity(good)
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("saving the column panicked: %v", r)
			}
		}()
		col = w.columnTo(w.chunks[ChunkPos{}], ChunkPos{})
	})
	if col == nil || len(col.Entities) != 1 || col.Entities[0].ID != good.UniqueID() {
		t.Fatalf("column saved = %+v, want it with the other entity only", col)
	}
	faults := sink.all(FaultSave)
	if len(faults) != 1 || faults[0].Entity != bad || faults[0].Type != "dragonfly:test_entity" || faults[0].Pos != (cube.Pos{1, 4, 1}) {
		t.Fatalf("faults = %v, want one blamed on the entity", sink.all(0))
	}
}
