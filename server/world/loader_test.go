package world

import (
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
