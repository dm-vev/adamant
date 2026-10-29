package entity_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// moveRecorder has an entity where the moves it is shown put it, as a
// client has it.
type moveRecorder struct {
	world.NopViewer
	pos                           map[*world.EntityHandle]mgl64.Vec3
	moves, absolute, vel, motions int
}

func (r *moveRecorder) ViewEntity(e world.Entity) { r.pos[e.H()] = e.Position() }
func (r *moveRecorder) ViewEntityMovement(e world.Entity, pos mgl64.Vec3, _ cube.Rotation, _ bool) {
	r.absolute++
	r.pos[e.H()] = pos
}
func (r *moveRecorder) ViewEntityVelocity(world.Entity, mgl64.Vec3) { r.vel++ }
func (r *moveRecorder) ViewEntityMove(e world.Entity, m *world.EntityMove) {
	if m.Absolute {
		r.absolute++
		r.pos[e.H()] = m.Pos
		return
	}
	if m.Changed&world.MoveVelocity != 0 {
		r.motions++
	}
	if m.Changed&^world.MoveVelocity == 0 {
		return
	}
	r.moves++
	p := r.pos[e.H()]
	for i, part := range [...]world.MoveParts{world.MoveX, world.MoveY, world.MoveZ} {
		if m.Changed&part != 0 {
			p[i] = m.Pos[i]
		}
	}
	r.pos[e.H()] = p
}

// TestMobMovesAsBDS lets a mob that was knocked into the air land and walk
// off: its viewer is shown a move every few ticks rather than one and a
// velocity on every tick, and has it where it is once it stood still.
func TestMobMovesAsBDS(t *testing.T) {
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	recorder := &moveRecorder{pos: map[*world.EntityHandle]mgl64.Vec3{}}
	var h *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		for x := range 32 {
			for z := range 32 {
				tx.SetBlock(cube.Pos{x, 0, z}, block.Stone{}, nil)
			}
		}
		loader := world.NewLoader(2, w, recorder)
		loader.Move(tx, mgl64.Vec3{16, 1, 16})
		loader.Load(tx, 25)
		h = entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{16.5, 1, 16.5}, Velocity: mgl64.Vec3{0.4, 0.42, 0.1}})
		tx.AddEntity(h)
	})
	const ticks = 200
	for range ticks {
		w.AdvanceTick()
	}
	var pos mgl64.Vec3
	<-w.Exec(func(tx *world.Tx) {
		e, ok := h.Entity(tx)
		if !ok {
			t.Fatal("the cow is gone")
		}
		pos = e.Position()
	})
	t.Logf("%d moves and %d absolute in %d ticks", recorder.moves, recorder.absolute, ticks)
	if recorder.moves == 0 || recorder.moves > ticks/2 {
		t.Fatalf("%d moves in %d ticks", recorder.moves, ticks)
	}
	if recorder.vel != 0 {
		t.Fatalf("%d velocities shown on their own, which the moves carry", recorder.vel)
	}
	got := recorder.pos[h]
	for i := range 3 {
		if math.Abs(got[i]-pos[i]) > 1.0/4096 {
			t.Fatalf("the viewer has the cow at %v, it is at %v (%d moves, %d absolute)", got, pos, recorder.moves, recorder.absolute)
		}
	}
}

// TestItemMovesAsBDS drops an item: its viewer is shown a move on every tick
// it falls, at once, its velocity on a few of them, and nothing once it lies
// still, where the viewer has it.
func TestItemMovesAsBDS(t *testing.T) {
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	recorder := &moveRecorder{pos: map[*world.EntityHandle]mgl64.Vec3{}}
	var h *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		for x := range 16 {
			for z := range 16 {
				tx.SetBlock(cube.Pos{x, 0, z}, block.Stone{}, nil)
			}
		}
		loader := world.NewLoader(2, w, recorder)
		loader.Move(tx, mgl64.Vec3{8, 1, 8})
		loader.Load(tx, 25)
		h = entity.NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{8.5, 12, 8.5}, Velocity: mgl64.Vec3{0.1, 0.2, 0}}, item.NewStack(item.Stick{}, 1))
		tx.AddEntity(h)
	})
	falling := 0
	for range 30 {
		before := recorder.moves
		w.AdvanceTick()
		if recorder.moves == before {
			break
		}
		falling++
	}
	if falling < 15 {
		t.Fatalf("shown %d moves on as many ticks while it fell, want one on every tick", falling)
	}
	for range 60 {
		w.AdvanceTick()
	}
	moves := recorder.moves
	for range 40 {
		w.AdvanceTick()
	}
	if recorder.moves != moves {
		t.Fatalf("%d moves of an item lying still", recorder.moves-moves)
	}
	if recorder.motions == 0 || recorder.motions > 4 || recorder.vel != 0 || recorder.absolute != 0 {
		t.Fatalf("%d velocities with the moves, %d alone, %d absolute moves", recorder.motions, recorder.vel, recorder.absolute)
	}
	var pos mgl64.Vec3
	<-w.Exec(func(tx *world.Tx) {
		e, ok := h.Entity(tx)
		if !ok {
			t.Error("the item is gone")
			return
		}
		pos = e.Position()
	})
	got := recorder.pos[h]
	for i := range 3 {
		if math.Abs(got[i]-pos[i]) > 1.0/4096 {
			t.Fatalf("the viewer has the item at %v, it is at %v", got, pos)
		}
	}
}
