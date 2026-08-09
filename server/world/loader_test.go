package world

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/go-gl/mathgl/mgl64"
)

// nopViewer implements Viewer with no-ops to avoid depending on the production
// session implementation for tests.
type nopViewer struct{ NopViewer }

func TestLoaderLoadsOuterRing(t *testing.T) {
	conf := Config{
		Dim:       Overworld,
		Provider:  NopProvider{},
		Generator: NopGenerator{},
	}
	w := conf.New()
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Fatalf("failed closing world: %v", err)
		}
	})

	loader := NewLoader(2, w, nopViewer{})

	<-w.Exec(func(tx *Tx) {
		loader.Move(tx, mgl64.Vec3{})
	})

	target := ChunkPos{2, 0}
	deadline := time.Now().Add(5 * time.Second)
	for {
		<-w.Exec(func(tx *Tx) {
			loader.Load(tx, 32)
		})
		if _, ok := loader.Chunk(target); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("chunk %v was never loaded", target)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestLoaderEvictsChunksOutsideRadius(t *testing.T) {
	conf := Config{
		Dim:       Overworld,
		Provider:  NopProvider{},
		Generator: NopGenerator{},
	}
	w := conf.New()
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Fatalf("failed closing world: %v", err)
		}
	})

	loader := NewLoader(2, w, nopViewer{})

	<-w.Exec(func(tx *Tx) {
		loader.Move(tx, mgl64.Vec3{})
	})

	target := ChunkPos{2, 1}
	deadline := time.Now().Add(5 * time.Second)
	for {
		<-w.Exec(func(tx *Tx) {
			loader.Load(tx, 32)
		})
		if _, ok := loader.Chunk(target); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("chunk %v was never loaded", target)
		}
		time.Sleep(10 * time.Millisecond)
	}

	<-w.Exec(func(tx *Tx) {
		loader.Move(tx, mgl64.Vec3{0, 0, 32})
	})

	if _, ok := loader.Chunk(target); ok {
		t.Fatalf("chunk %v was not evicted after moving outside radius", target)
	}
}

func TestLoaderChangeWorldDoesNotBlockViewChunkCompletion(t *testing.T) {
	old := New()
	newWorld := New()
	t.Cleanup(func() {
		_ = old.Close()
		_ = newWorld.Close()
	})
	loader := NewLoader(1, old, nopViewer{})

	viewStarted := make(chan struct{})
	releaseView := make(chan struct{})
	old.exec(func(tx *Tx) {
		close(viewStarted)
		<-releaseView
		loader.viewChunk(tx, ChunkPos{}, nil)
	})
	<-viewStarted

	change := newWorld.Do(func(tx *Tx) {
		loader.ChangeWorld(tx, newWorld)
	})
	deadline := time.Now().Add(time.Second)
	for {
		if loader.mu.TryRLock() {
			changing := loader.changing
			loader.mu.RUnlock()
			if changing {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("loader did not begin world migration")
		}
		time.Sleep(time.Millisecond)
	}
	close(releaseView)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := change.Wait(ctx); err != nil {
		t.Fatalf("change world blocked behind chunk completion: %v", err)
	}
	if got := loader.World(); got != newWorld {
		t.Fatalf("loader world = %p, want %p", got, newWorld)
	}
	if _, ok := loader.Chunk(ChunkPos{}); ok {
		t.Fatal("loader retained a chunk from the old world")
	}
}

func TestLoaderChangeWorldAfterOldWorldClosed(t *testing.T) {
	old := New()
	newWorld := New()
	t.Cleanup(func() {
		_ = newWorld.Close()
	})
	loader := NewLoader(1, old, nopViewer{})
	if err := old.Close(); err != nil {
		t.Fatalf("close old world: %v", err)
	}

	change := newWorld.Do(func(tx *Tx) {
		loader.ChangeWorld(tx, newWorld)
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := change.Wait(ctx); err != nil {
		t.Fatalf("change from closed world: %v", err)
	}
	if got := loader.World(); got != newWorld {
		t.Fatalf("loader world = %p, want %p", got, newWorld)
	}
	old.viewerMu.Lock()
	_, oldRegistered := old.viewers[loader]
	old.viewerMu.Unlock()
	if oldRegistered {
		t.Fatal("loader remained registered in closed world")
	}
}

func TestLoaderCloseSerialisedWithChangeWorld(t *testing.T) {
	old := Config{Synchronous: true}.New()
	newWorld := Config{Synchronous: true}.New()
	t.Cleanup(func() {
		_ = old.Close()
		_ = newWorld.Close()
	})
	loader := NewLoader(1, old, nopViewer{})

	// Hold mu so Close deterministically acquires changeMu before ChangeWorld starts.
	loader.mu.Lock()
	closeDone := make(chan struct{})
	go func() {
		old.Do(func(tx *Tx) { loader.Close(tx) })
		close(closeDone)
	}()
	deadline := time.Now().Add(time.Second)
	for loader.changeMu.TryLock() {
		loader.changeMu.Unlock()
		if time.Now().After(deadline) {
			loader.mu.Unlock()
			t.Fatal("Close did not acquire the loader change lock")
		}
	}

	changeDone := make(chan struct{})
	go func() {
		newWorld.Do(func(tx *Tx) { loader.ChangeWorld(tx, newWorld) })
		close(changeDone)
	}()
	loader.mu.Unlock()

	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("Close blocked against ChangeWorld")
	}
	select {
	case <-changeDone:
	case <-time.After(time.Second):
		t.Fatal("ChangeWorld blocked after Close")
	}

	if got := loader.World(); got != old {
		t.Fatalf("closed loader moved to world %p, want %p", got, old)
	}
	newWorld.viewerMu.Lock()
	_, registered := newWorld.viewers[loader]
	newWorld.viewerMu.Unlock()
	if registered {
		t.Fatal("closed loader registered in destination world")
	}
}

func TestLoaderCloseRemovesChunksFromLoaderWorld(t *testing.T) {
	old := Config{Synchronous: true}.New()
	current := Config{Synchronous: true}.New()
	t.Cleanup(func() {
		_ = old.Close()
		_ = current.Close()
	})
	loader := NewLoader(1, old, nopViewer{})
	pos := ChunkPos{}
	var col *Column
	old.Do(func(tx *Tx) {
		col = tx.chunk(pos)
		loader.viewChunk(tx, pos, col)
	})

	current.Do(func(tx *Tx) {
		loader.Close(tx)
	})

	for _, registered := range col.loaders {
		if registered == loader {
			t.Fatal("closed loader remained registered in its old world chunk")
		}
	}
}

func TestLoaderEvictionClosesUnusedChunks(t *testing.T) {
	const radius = 2
	conf := Config{
		Dim:       Overworld,
		Provider:  NopProvider{},
		Generator: NopGenerator{},
	}
	w := conf.New()
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Fatalf("failed closing world: %v", err)
		}
	})

	loader := NewLoader(radius, w, nopViewer{})

	expected := chunksWithinRadius(radius)

	loadAll := func(deadline time.Time) {
		for {
			<-w.Exec(func(tx *Tx) {
				loader.Load(tx, 64)
			})

			loader.mu.RLock()
			queueLen := len(loader.loadQueue)
			loaded := len(loader.loaded)
			loader.mu.RUnlock()

			if queueLen == 0 {
				if loaded != expected {
					t.Fatalf("expected %d loaded chunks, got %d", expected, loaded)
				}
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("loader did not finish loading chunks in time")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Prime the loader at the origin.
	<-w.Exec(func(tx *Tx) {
		loader.Move(tx, mgl64.Vec3{})
	})
	loadAll(time.Now().Add(5 * time.Second))

	// Repeatedly move to a new location and ensure we don't retain chunks from the previous centre.
	for step := 1; step <= 4; step++ {
		<-w.Exec(func(tx *Tx) {
			loader.Move(tx, mgl64.Vec3{float64(step * 64), 0, 0})
		})
		loadAll(time.Now().Add(5 * time.Second))

		deadline := time.Now().Add(5 * time.Second)
		for {
			if got := w.LoadedChunkCount(); got <= expected {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("world retained %d chunks after evicting loader", w.LoadedChunkCount())
			}
			time.Sleep(10 * time.Millisecond)
		}
		if got := w.LoadedChunkCount(); got > expected {
			t.Fatalf("world retained %d chunks after evicting loader", got)
		}
	}
}

func chunksWithinRadius(r int) int {
	var count int
	for x := -r; x <= r; x++ {
		for z := -r; z <= r; z++ {
			if int(math.Round(math.Sqrt(float64(x*x+z*z)))) <= r {
				count++
			}
		}
	}
	return count
}
