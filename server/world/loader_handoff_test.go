package world

import (
	"context"
	"testing"
	"time"

	"github.com/go-gl/mathgl/mgl64"
)

func TestLoaderForeignWorldOperationsDoNotSuspendOwner(t *testing.T) {
	for _, operation := range []string{"change", "close"} {
		t.Run(operation, func(t *testing.T) {
			old := Config{GeneratorWorkers: 1}.New()
			current := Config{Dim: Nether, GeneratorWorkers: 1}.New()
			loader := NewLoader(0, old, NopViewer{})
			busy, release := make(chan struct{}), make(chan struct{})
			released := false
			defer func() {
				if !released {
					close(release)
				}
				_ = old.Close()
				_ = current.Close()
			}()
			old.Do(func(*Tx) { close(busy); <-release })
			<-busy
			changed := current.Do(func(tx *Tx) {
				if operation == "change" {
					loader.ChangeWorld(tx, current)
				} else {
					loader.Close(tx)
				}
				tx.World().SetTime(6000)
			})
			completed := false
			select {
			case <-changed.Done():
				completed = true
			case <-time.After(time.Second):
			}
			observed := current.Time()
			close(release)
			released = true
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := changed.Wait(ctx); err != nil {
				t.Fatalf("operation did not finish: %v", err)
			}
			if !completed || observed < 6000 {
				t.Fatalf("%s suspended current world: completed=%v time=%d", operation, completed, observed)
			}
		})
	}
}

func TestLoaderRapidChangesWaitForAllOldViews(t *testing.T) {
	for _, closeDuringChange := range []bool{false, true} {
		name := "publish"
		if closeDuringChange {
			name = "close"
		}
		t.Run(name, func(t *testing.T) {
			old := Config{GeneratorWorkers: 1}.New()
			middle := Config{Dim: Nether, Synchronous: true}.New()
			current := Config{Dim: End, Synchronous: true}.New()
			viewer := &handoffViewer{chunks: make(chan Dimension, 8), hides: make(chan *EntityHandle, 8)}
			loader := NewLoader(0, old, viewer)
			handle := EntitySpawnOpts{Position: mgl64.Vec3{0.5, 4, 0.5}}.New(reentrantEntityType{}, reentrantEntityConfig{})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := old.Do(func(tx *Tx) { tx.AddEntity(handle); loader.Load(tx, 1) }).Wait(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case <-viewer.chunks:
			case <-ctx.Done():
				t.Fatal("initial chunk was not published")
			}
			busy, release := make(chan struct{}), make(chan struct{})
			released := false
			defer func() {
				if !released {
					close(release)
				}
				_ = old.Close()
				_ = middle.Close()
				_ = current.Close()
			}()
			old.Do(func(*Tx) { close(busy); <-release })
			<-busy
			middle.Do(func(tx *Tx) { loader.ChangeWorld(tx, middle) })
			current.Do(func(tx *Tx) {
				loader.ChangeWorld(tx, current)
				if closeDuringChange {
					loader.Close(tx)
				}
				loader.Load(tx, 1)
			})
			select {
			case dim := <-viewer.chunks:
				t.Errorf("published %v before the original view was detached", dim)
			default:
			}
			close(release)
			released = true
			select {
			case hidden := <-viewer.hides:
				if hidden != handle {
					t.Fatalf("hid %p, want original entity %p", hidden, handle)
				}
			case <-ctx.Done():
				t.Fatal("original entity was not hidden after detachment")
			}
			if closeDuringChange {
				current.Do(func(tx *Tx) { loader.Load(tx, 1) })
				if _, ok := loader.Chunk(ChunkPos{}); ok {
					t.Fatal("closed loader published a replacement view")
				}
				return
			}
			deadline := time.Now().Add(3 * time.Second)
			for {
				current.Do(func(tx *Tx) { loader.Load(tx, 1) })
				if _, ok := loader.Chunk(ChunkPos{}); ok {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("replacement view remained blocked after all detaches")
				}
				time.Sleep(time.Millisecond)
			}
			if dim := <-viewer.chunks; dim != End {
				t.Fatalf("replacement dimension = %v, want End", dim)
			}
		})
	}
}

func TestLoaderViewCallbacksCanReadLoaderDuringRemoval(t *testing.T) {
	for _, operation := range []string{"move", "radius", "close", "change"} {
		t.Run(operation, func(t *testing.T) {
			w := Config{Synchronous: true}.New()
			other := Config{Dim: Nether, Synchronous: true}.New()
			defer w.Close()
			defer other.Close()
			viewer := &handoffViewer{chunks: make(chan Dimension, 8), hides: make(chan *EntityHandle, 8)}
			loader := NewLoader(1, w, viewer)
			handle := EntitySpawnOpts{Position: mgl64.Vec3{16.5, 4, 0.5}}.New(reentrantEntityType{}, reentrantEntityConfig{})
			w.Do(func(tx *Tx) { tx.AddEntity(handle); loader.viewChunk(tx, ChunkPos{1, 0}, tx.chunk(ChunkPos{1, 0})) })
			blocked := false
			viewer.reenter = func() {
				done := make(chan struct{})
				go func() { loader.World(); loader.Chunk(ChunkPos{1, 0}); close(done) }()
				select {
				case <-done:
				case <-time.After(time.Second):
					blocked = true
				}
			}
			w.Do(func(tx *Tx) {
				switch operation {
				case "move":
					loader.Move(tx, mgl64.Vec3{64, 0, 0})
				case "radius":
					loader.ChangeRadius(tx, 0)
				case "close":
					loader.Close(tx)
				case "change":
					loader.ChangeWorld(tx, other)
				}
			})
			if blocked {
				t.Fatal("viewer callback could not re-enter loader read APIs")
			}
			select {
			case hidden := <-viewer.hides:
				if hidden != handle {
					t.Fatal("removed view did not hide its entity")
				}
			default:
				t.Fatal("entity was not hidden on view removal")
			}
		})
	}
}

type handoffViewer struct {
	NopViewer
	chunks  chan Dimension
	hides   chan *EntityHandle
	reenter func()
}

func (v *handoffViewer) ViewChunk(_ ChunkPos, dim Dimension, _ *Column) { v.chunks <- dim }
func (v *handoffViewer) ViewTime(int) {
	if v.reenter != nil {
		v.reenter()
	}
}
func (v *handoffViewer) HideEntity(e Entity) {
	if v.reenter != nil {
		v.reenter()
	}
	v.hides <- e.H()
}
