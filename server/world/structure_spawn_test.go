package world

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/internal/faultcheck"
	"github.com/df-mc/dragonfly/server/block/cube"
)

type mockSpawnStructureGenerator struct {
	NopGenerator
	fn func(pos ChunkPos) []StructureArea
}

func (m *mockSpawnStructureGenerator) SpawnStructures(pos ChunkPos) []StructureArea {
	return m.fn(pos)
}

func TestSpawnStructuresAsyncNonBlocking(t *testing.T) {
	started := make(chan ChunkPos, 4)
	release := make(chan struct{})
	var calls atomic.Int32
	want := []StructureArea{{
		ID:     "minecraft:swamp_hut",
		Box:    [2]cube.Pos{{0, 60, 0}, {7, 67, 7}},
		Pieces: [][2]cube.Pos{{{0, 60, 0}, {7, 67, 7}}},
	}}
	gen := &mockSpawnStructureGenerator{
		fn: func(pos ChunkPos) []StructureArea {
			calls.Add(1)
			started <- pos
			<-release
			return want
		},
	}
	w := Config{Generator: gen}.New()
	defer w.Close()

	pos := ChunkPos{3, -5}
	start := time.Now()
	got := w.SpawnStructures(pos)
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("async SpawnStructures blocked caller for %v", elapsed)
	}
	if got != nil {
		t.Fatalf("expected nil on cold async cache miss, got %+v", got)
	}

	select {
	case gotPos := <-started:
		if gotPos != pos {
			t.Fatalf("worker received %v, want %v", gotPos, pos)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("background spawn-structure worker did not start")
	}

	// Concurrent and repeated misses for the same chunk must be deduplicated.
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 16 {
				if areas := w.SpawnStructures(pos); areas != nil {
					t.Errorf("unexpected result before worker finished: %+v", areas)
				}
			}
		}()
	}
	wg.Wait()
	if n := calls.Load(); n != 1 {
		t.Fatalf("finder invoked %d times for same pending chunk, want 1", n)
	}

	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if areas := w.SpawnStructures(pos); len(areas) == 1 && areas[0].ID == want[0].ID {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cached spawn structures never became visible after worker completed")
		}
		time.Sleep(time.Millisecond)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("finder invoked %d times after cache populated, want 1", n)
	}
}

func TestSpawnStructuresSynchronous(t *testing.T) {
	var calls atomic.Int32
	want := []StructureArea{{
		ID:  "minecraft:pillager_outpost",
		Box: [2]cube.Pos{{16, 64, 16}, {31, 80, 31}},
	}}
	gen := &mockSpawnStructureGenerator{
		fn: func(pos ChunkPos) []StructureArea {
			calls.Add(1)
			return want
		},
	}
	w := Config{Generator: gen, Synchronous: true}.New()
	defer w.Close()

	pos := ChunkPos{1, 1}
	got := w.SpawnStructures(pos)
	if len(got) != 1 || got[0].ID != want[0].ID {
		t.Fatalf("synchronous SpawnStructures = %+v, want %+v", got, want)
	}
	got2 := w.SpawnStructures(pos)
	if len(got2) != 1 || got2[0].ID != want[0].ID {
		t.Fatalf("cached synchronous SpawnStructures = %+v, want %+v", got2, want)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("finder invoked %d times in synchronous mode, want 1", n)
	}
}

func TestSpawnStructuresCloseWaitsAndCancelsQueued(t *testing.T) {
	started := make(chan ChunkPos, spawnStructureWorkers*2)
	release := make(chan struct{})
	var calls atomic.Int32
	gen := &mockSpawnStructureGenerator{
		fn: func(pos ChunkPos) []StructureArea {
			calls.Add(1)
			started <- pos
			<-release
			return nil
		},
	}
	w := Config{Generator: gen}.New()

	// Fill all workers and enqueue extra chunks behind them.
	for i := range int32(spawnStructureWorkers + 10) {
		w.SpawnStructures(ChunkPos{i, 0})
	}
	for range spawnStructureWorkers {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("workers did not fill concurrency slots")
		}
	}

	closed := make(chan error, 1)
	go func() {
		closed <- w.Close()
	}()

	// Close must wait while in-flight workers are still running.
	select {
	case err := <-closed:
		t.Fatalf("Close returned before in-flight spawn-structure workers exited: %v", err)
	case <-time.After(30 * time.Millisecond):
	}

	close(release)
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not complete after workers were released")
	}

	// Queued items beyond the in-flight workers must not have been started after Close.
	if n := calls.Load(); n > spawnStructureWorkers {
		t.Fatalf("finder ran %d times after Close, want at most %d", n, spawnStructureWorkers)
	}
	// Calls after Close must be no-ops.
	if got := w.SpawnStructures(ChunkPos{999, 999}); got != nil {
		t.Fatalf("SpawnStructures after Close returned %+v, want nil", got)
	}
}

func TestSpawnStructuresAsyncPanicRecovery(t *testing.T) {
	var attempts atomic.Int32
	want := []StructureArea{{ID: "minecraft:monument"}}
	doneFirst := make(chan struct{})
	gen := &mockSpawnStructureGenerator{
		fn: func(pos ChunkPos) []StructureArea {
			if attempts.Add(1) == 1 {
				defer close(doneFirst)
				panic(faultcheck.Expected{Why: "test: simulated finder failure"})
			}
			return want
		},
	}
	w := Config{Generator: gen}.New()
	defer w.Close()

	pos := ChunkPos{-2, 4}
	w.SpawnStructures(pos)
	select {
	case <-doneFirst:
	case <-time.After(2 * time.Second):
		t.Fatal("first attempt did not run")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if areas := w.SpawnStructures(pos); len(areas) == 1 && areas[0].ID == want[0].ID {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("retry after panic never populated cache")
		}
		time.Sleep(2 * time.Millisecond)
	}
}
