package world

import (
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
