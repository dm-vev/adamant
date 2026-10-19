package world

import (
	"context"
	"testing"
	"time"

	"github.com/go-gl/mathgl/mgl64"
)

func TestAddEntityDoesNotWaitForTerrainGeneration(t *testing.T) {
	for _, tracked := range []bool{false, true} {
		name := "new_chunk"
		if tracked {
			name = "generating_chunk"
		}
		t.Run(name, func(t *testing.T) {
			pos := ChunkPos{4000, 4000}
			generator := heldGenerator{held: pos, release: make(chan struct{})}
			w := Config{Generator: generator, GeneratorWorkers: 1}.New()
			released := false
			defer func() {
				if !released {
					close(generator.release)
				}
				_ = w.Close()
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if tracked {
				if err := w.Do(func(*Tx) {
					if _, err := w.loadChunk(pos); err != nil {
						panic(err)
					}
				}).Wait(ctx); err != nil {
					t.Fatal(err)
				}
			}
			handle := EntitySpawnOpts{Position: mgl64.Vec3{64000.5, 64, 64000.5}}.New(reentrantEntityType{}, reentrantEntityConfig{})
			attached := w.Do(func(tx *Tx) {
				tx.AddEntity(handle)
				w.SetTime(6000)
			})
			completed := false
			select {
			case <-attached.Done():
				completed = true
			case <-time.After(time.Second):
			}
			observed := w.Time()
			close(generator.release)
			released = true
			if err := attached.Wait(ctx); err != nil {
				t.Fatal(err)
			}
			if !completed || observed < 6000 {
				t.Fatalf("entity attachment suspended owner: completed=%v time=%d", completed, observed)
			}
			if err := w.Do(func(tx *Tx) {
				// Await only after releasing the generator: terrain publication must
				// retain the entity already attached to the column's owner-only list.
				col := tx.chunk(pos)
				if len(col.Entities) != 1 || col.Entities[0] != handle {
					t.Errorf("generation lost or duplicated attached entity: %v", col.Entities)
				}
				if _, ok := handle.Entity(tx); !ok {
					t.Error("entity left its destination world during publication")
				}
				if w.EntityCount() != 1 {
					t.Errorf("entity count = %d, want 1", w.EntityCount())
				}
				tx.RemoveEntity(handle.mustEntity(tx))
				_ = handle.Close()
			}).Wait(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}
