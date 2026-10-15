package world

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

type closeTrackingEntityType struct{ closed *bool }

func (t closeTrackingEntityType) Open(_ *Tx, handle *EntityHandle, data *EntityData) Entity {
	return &closeTrackingEntity{handle: handle, data: data, closed: t.closed}
}
func (closeTrackingEntityType) EncodeEntity() string                  { return "dragonfly:close_tracking" }
func (closeTrackingEntityType) BBox(Entity) cube.BBox                 { return cube.Box(0, 0, 0, 1, 1, 1) }
func (closeTrackingEntityType) DecodeNBT(map[string]any, *EntityData) {}
func (closeTrackingEntityType) EncodeNBT(*EntityData) map[string]any  { return nil }

type closeTrackingEntity struct {
	handle *EntityHandle
	data   *EntityData
	closed *bool
}

func (e *closeTrackingEntity) Close() error            { *e.closed = true; return nil }
func (e *closeTrackingEntity) H() *EntityHandle        { return e.handle }
func (e *closeTrackingEntity) Position() mgl64.Vec3    { return e.data.Pos }
func (e *closeTrackingEntity) Rotation() cube.Rotation { return e.data.Rot }

// TestTeleportedEntitySurvivesUnloadOfChunkItLeft teleports an entity far
// away and moves the only loader after it before the entity ticks, so the
// chunk it left unloads while still listing it. The entity moves to its new
// chunk instead of closing with the old one, which kicked teleported players.
func TestTeleportedEntitySurvivesUnloadOfChunkItLeft(t *testing.T) {
	w := Config{Synchronous: true, Provider: NopProvider{}, Generator: NopGenerator{}}.New()
	t.Cleanup(func() { _ = w.Close() })
	l := NewLoader(0, w, NopViewer{})
	closed := false
	h := EntitySpawnOpts{Position: mgl64.Vec3{0.5, 4, 0.5}}.New(closeTrackingEntityType{closed: &closed}, reentrantEntityConfig{})
	target := mgl64.Vec3{5*16 + 0.5, 4, 0.5}
	<-w.Exec(func(tx *Tx) {
		l.Move(tx, mgl64.Vec3{0.5, 4, 0.5})
		l.Load(tx, 1)
		tx.AddEntity(h)
	})
	<-w.Exec(func(tx *Tx) {
		h.data.Pos = target
		l.Move(tx, target)
	})
	<-w.Exec(func(tx *Tx) {
		if closed {
			t.Fatal("teleported entity closed with the chunk it left")
		}
		if _, ok := h.Entity(tx); !ok {
			t.Fatal("teleported entity is no longer in the world")
		}
		if got := w.entities[h].pos; got != (ChunkPos{5, 0}) {
			t.Fatalf("entity listed in chunk %v, want [5 0]", got)
		}
	})
}
