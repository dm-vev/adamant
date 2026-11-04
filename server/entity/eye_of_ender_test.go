package entity

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
)

// eyeRecorder has where a viewer was shown the eye of ender, and the
// particles it was shown.
type eyeRecorder struct {
	world.NopViewer
	pos       mgl64.Vec3
	moves     int
	particles map[world.Particle]mgl64.Vec3
}

func (r *eyeRecorder) ViewEntityMovement(_ world.Entity, pos mgl64.Vec3, _ cube.Rotation, _ bool) {
	r.pos = pos
	r.moves++
}
func (r *eyeRecorder) ViewEntityMove(_ world.Entity, m *world.EntityMove) {
	// The moves are relative to the last absolute one only in the parts
	// that changed.
	for i, part := range [...]world.MoveParts{world.MoveX, world.MoveY, world.MoveZ} {
		if m.Absolute || m.Changed&part != 0 {
			r.pos[i] = m.Pos[i]
		}
	}
	r.moves++
}
func (r *eyeRecorder) ViewParticle(pos mgl64.Vec3, p world.Particle) { r.particles[p] = pos }
func (r *eyeRecorder) ViewParticleShared(pos mgl64.Vec3, p world.Particle, _ *world.Broadcast) {
	r.particles[p] = pos
}

// throwEye throws an eye of ender of conf from the origin at y 70, seen by a
// recorder, and ticks the world 81 times: Java
// EyeOfEnder.tick resolves the eye on its 81st tick. It returns the recorder
// and the item entities left in the world, and where the eye last was.
func throwEye(t *testing.T, conf world.EntityConfig) (*eyeRecorder, []item.Stack, mgl64.Vec3) {
	t.Helper()
	w := world.Config{Synchronous: true, Entities: DefaultRegistry, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	recorder := &eyeRecorder{particles: map[world.Particle]mgl64.Vec3{}}
	start := mgl64.Vec3{0.5, 70, 0.5}
	h := world.EntitySpawnOpts{Position: start}.New(EyeOfEnderType, conf)
	<-w.Exec(func(tx *world.Tx) {
		loader := world.NewLoader(2, w, recorder)
		loader.Move(tx, start)
		loader.Load(tx, 25)
		tx.AddEntity(h)
	})
	var last mgl64.Vec3
	for i := range 81 {
		<-w.Exec(func(tx *world.Tx) {
			e, ok := h.Entity(tx)
			if !ok {
				t.Fatalf("the eye is gone after %d ticks", i)
			}
			last = e.Position()
		})
		w.AdvanceTick()
	}
	var items []item.Stack
	<-w.Exec(func(tx *world.Tx) {
		if _, ok := h.Entity(tx); ok {
			t.Fatal("the eye is still there after 81 ticks")
		}
		for e := range tx.Entities() {
			if e.H().Type() == ItemType {
				items = append(items, e.(*Ent).Behaviour().(*ItemBehaviour).Item())
			}
		}
	})
	return recorder, items, last
}

// TestEyeOfEnderFliesAndDrops has a thrown eye fly towards its target, shown
// to its viewers as it goes, and drop as an item where it ended, as Java
// EyeOfEnder does with four in five eyes.
func TestEyeOfEnderFliesAndDrops(t *testing.T) {
	t.Parallel()
	target := mgl64.Vec3{10.5, 75, 0.5}
	recorder, items, last := throwEye(t, eyeOfEnderConfig{target: target, survive: true})
	if recorder.moves == 0 {
		t.Fatal("the viewer was not shown the eye flying")
	}
	if !recorder.pos.ApproxEqualThreshold(last, 1e-3) {
		t.Errorf("the viewer has the eye at %v, want %v", recorder.pos, last)
	}
	if math.Hypot(target[0]-last[0], target[2]-last[2]) > 1 || last[1] <= 70 {
		t.Errorf("the eye ended at %v, want it flown up towards %v", last, target)
	}
	if len(items) != 1 || items[0].Count() != 1 || items[0].Item() != (item.EnderEye{}) {
		t.Fatalf("items dropped = %v, want one eye of ender", items)
	}
	if _, ok := recorder.particles[particle.EyeOfEnderDeath{}]; ok {
		t.Error("a dropped eye shattered")
	}
}

// TestEyeOfEnderShatters has an eye that does not survive shatter with Java
// level event 2003, centred on the floor of its block, and drop nothing.
func TestEyeOfEnderShatters(t *testing.T) {
	t.Parallel()
	recorder, items, _ := throwEye(t, eyeOfEnderConfig{target: mgl64.Vec3{30.5, 75, 0.5}})
	if len(items) != 0 {
		t.Fatalf("a shattered eye dropped %v", items)
	}
	pos, ok := recorder.particles[particle.EyeOfEnderDeath{}]
	if !ok {
		t.Fatal("the eye did not shatter")
	}
	if pos != cube.PosFromVec3(pos).Vec3Middle() {
		t.Errorf("shattered at %v, want the middle of the floor of a block", pos)
	}
}

// reloadedEye leaves an eye of ender as decoding its saved NBT does: with no
// behaviour for its type to open it with.
type reloadedEye struct{}

func (reloadedEye) Apply(*world.EntityData) {}

// TestReloadedEyeOfEnderFliesOn reloads an eye, which Java saves without its
// target: it keeps the speed it had, and shatters.
func TestReloadedEyeOfEnderFliesOn(t *testing.T) {
	t.Parallel()
	vel := mgl64.Vec3{0.1, 0.05, 0}
	w := world.Config{Synchronous: true, Entities: DefaultRegistry, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	h := world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 70, 0.5}, Velocity: vel}.New(EyeOfEnderType, reloadedEye{})
	<-w.Exec(func(tx *world.Tx) {
		tx.AddEntity(h)
		e, _ := h.Entity(tx)
		for i := range 10 {
			e.(*EyeOfEnder).Tick(tx, int64(i))
		}
		if got := e.(*EyeOfEnder).Velocity(); got != vel {
			t.Errorf("velocity = %v, want %v kept", got, vel)
		}
		if got, want := e.Position(), (mgl64.Vec3{1.5, 70.5, 0.5}); !got.ApproxEqualThreshold(want, 1e-9) {
			t.Errorf("position = %v, want %v", got, want)
		}
	})
}
