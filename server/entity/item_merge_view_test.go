package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// viewRecorder counts entity updates sent for entities it was not shown.
type viewRecorder struct {
	world.NopViewer
	shown  map[*world.EntityHandle]bool
	unseen int
}

func (r *viewRecorder) ViewEntity(e world.Entity) { r.shown[e.H()] = true }
func (r *viewRecorder) HideEntity(e world.Entity) { delete(r.shown, e.H()) }
func (r *viewRecorder) ViewEntityMovement(e world.Entity, _ mgl64.Vec3, _ cube.Rotation, _ bool) {
	r.check(e)
}
func (r *viewRecorder) ViewEntityVelocity(e world.Entity, _ mgl64.Vec3) { r.check(e) }
func (r *viewRecorder) ViewEntityState(e world.Entity)                  { r.check(e) }

func (r *viewRecorder) check(e world.Entity) {
	if !r.shown[e.H()] {
		r.unseen++
	}
}

// TestMergedItemsSendNoUpdates checks that items removed by merging stop
// updating their viewers. A merging item used to finish its tick and send its
// movement after it was hidden, and the item it merged into was still ticked
// from its column: clients logged an unknown entity for every merge.
func TestMergedItemsSendNoUpdates(t *testing.T) {
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	recorder := &viewRecorder{shown: map[*world.EntityHandle]bool{}}
	<-w.Exec(func(tx *world.Tx) {
		for x := range 16 {
			for z := range 16 {
				tx.SetBlock(cube.Pos{x, 0, z}, block.Stone{}, nil)
			}
		}
		loader := world.NewLoader(2, w, recorder)
		loader.Move(tx, mgl64.Vec3{8, 1, 8})
		loader.Load(tx, 25)
		// Falling items merge while they still move.
		for i := range 6 {
			pos := mgl64.Vec3{7.5 + float64(i)*0.2, 12, 8.5}
			vel := mgl64.Vec3{0.02 * float64(i%2*2-1), 0, 0}
			tx.AddEntity(entity.NewItemPickupDelay(world.EntitySpawnOpts{Position: pos, Velocity: vel}, item.NewStack(item.Stick{}, 1), 0))
		}
	})
	for range 60 {
		w.AdvanceTick()
	}
	items := 0
	<-w.Exec(func(tx *world.Tx) {
		for e := range tx.Entities() {
			if e.H().Type() == entity.ItemType {
				items++
			}
		}
	})
	if items >= 6 {
		t.Fatalf("items did not merge: %d remain", items)
	}
	if recorder.unseen != 0 {
		t.Fatalf("viewer received %d updates for entities it was not shown", recorder.unseen)
	}
}
