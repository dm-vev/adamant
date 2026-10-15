package world

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

// heldGenerator generates nothing and holds generating one chunk until
// released.
type heldGenerator struct {
	held    ChunkPos
	release chan struct{}
}

func (g heldGenerator) GenerateChunk(pos ChunkPos, _ *chunk.Chunk, _ GenerationContext) GenerationResult {
	if pos == g.held {
		<-g.release
	}
	return GenerationResult{}
}
func (heldGenerator) DefaultSpawn(Dimension) cube.Pos { return cube.Pos{} }

// TestTeleportIntoGeneratingChunkKeepsWorldTicking teleports an entity into a
// chunk that is still generating. The entity waits for the chunk, as Java
// entities in chunks that are not loaded do, instead of the whole world
// waiting on the generator.
func TestTeleportIntoGeneratingChunkKeepsWorldTicking(t *testing.T) {
	far := ChunkPos{4000, 4000}
	gen := heldGenerator{held: far, release: make(chan struct{})}
	w := Config{Provider: NopProvider{}, Generator: gen}.New()
	l := NewLoader(1, w, NopViewer{})
	t.Cleanup(func() {
		<-w.Exec(func(tx *Tx) { l.Close(tx) })
		_ = w.Close()
	})
	closed := false
	h := EntitySpawnOpts{Position: mgl64.Vec3{0.5, 4, 0.5}}.New(closeTrackingEntityType{closed: &closed}, reentrantEntityConfig{})
	<-w.Exec(func(tx *Tx) {
		l.Move(tx, mgl64.Vec3{0.5, 4, 0.5})
		l.Load(tx, 9)
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		var ready bool
		<-w.Exec(func(tx *Tx) { ready = tx.ChunkLoaded(ChunkPos{}) })
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("spawn chunk never generated")
		}
		time.Sleep(10 * time.Millisecond)
	}
	<-w.Exec(func(tx *Tx) {
		tx.AddEntity(h)
		h.data.Pos = mgl64.Vec3{float64(far[0]*16) + 0.5, 4, float64(far[1]*16) + 0.5}
	})
	for range 5 {
		select {
		case <-w.Exec(func(*Tx) {}):
		case <-time.After(2 * time.Second):
			close(gen.release)
			t.Fatal("world stopped while the teleported entity's chunk was generating")
		}
		time.Sleep(60 * time.Millisecond)
	}
	close(gen.release)
	deadline = time.Now().Add(5 * time.Second)
	for {
		var pos ChunkPos
		var ok bool
		<-w.Exec(func(tx *Tx) {
			_, ok = h.Entity(tx)
			pos = w.entities[h].pos
		})
		if !ok {
			t.Fatal("teleported entity left the world")
		}
		if pos == far {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("entity listed in chunk %v, want %v", pos, far)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
