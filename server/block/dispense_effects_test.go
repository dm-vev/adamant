package block_test

import (
	"log/slog"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// effectRecorder records the sounds and particles a viewer is shown.
type effectRecorder struct {
	world.NopViewer
	sounds    []recordedSound
	particles []recordedParticle
	order     []string
}

type recordedSound struct {
	pos mgl64.Vec3
	s   world.Sound
}

type recordedParticle struct {
	pos mgl64.Vec3
	p   world.Particle
}

func (r *effectRecorder) ViewSound(pos mgl64.Vec3, s world.Sound) {
	r.sounds = append(r.sounds, recordedSound{pos, s})
	r.order = append(r.order, "sound")
}

func (r *effectRecorder) ViewParticle(pos mgl64.Vec3, p world.Particle) {
	r.particles = append(r.particles, recordedParticle{pos, p})
	r.order = append(r.order, "particle")
}

// dispenserTest is a dispenser in a world that records what its viewer is
// shown, the way a player standing next to it sees the dispenser.
type dispenserTest struct {
	t        *testing.T
	w        *world.World
	recorder *effectRecorder
	pos      cube.Pos
	loader   *world.Loader
}

// newDispenserTest returns a world with a stone floor below y 64 and a
// dispenser at (0, 64, 0) that faces the face given.
func newDispenserTest(t *testing.T, facing cube.Face) *dispenserTest {
	t.Helper()
	w := world.Config{
		Log:             slog.New(slog.DiscardHandler),
		Synchronous:     true,
		RandomTickSpeed: -1,
		Entities:        entity.DefaultRegistry,
		RandSource:      rand.NewPCG(1, 2),
	}.New()
	t.Cleanup(func() { _ = w.Close() })
	dt := &dispenserTest{t: t, w: w, recorder: &effectRecorder{}, pos: cube.Pos{0, 64, 0}}
	dt.loader = world.NewLoader(1, w, dt.recorder)
	<-w.Exec(func(tx *world.Tx) {
		dt.loader.Load(tx, 9)
		for x := -3; x <= 3; x++ {
			for z := -3; z <= 3; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		d := block.NewDispenser()
		d.Facing = facing
		tx.SetBlock(dt.pos, d, nil)
	})
	return dt
}

// front returns the position the dispenser faces.
func (dt *dispenserTest) front() cube.Pos {
	var f cube.Pos
	<-dt.w.Exec(func(tx *world.Tx) { f = dt.pos.Side(tx.Block(dt.pos).(block.Dispenser).Facing) })
	return f
}

// dispense puts the stacks into the dispenser, from its first slot, and runs
// its scheduled tick; set runs first, to build what surrounds the dispenser.
// It returns the effects of the tick alone.
func (dt *dispenserTest) dispense(set func(tx *world.Tx, front cube.Pos), stacks ...item.Stack) {
	dt.t.Helper()
	front := dt.front()
	<-dt.w.Exec(func(tx *world.Tx) {
		if set != nil {
			set(tx, front)
		}
		d := tx.Block(dt.pos).(block.Dispenser)
		for i, s := range stacks {
			_ = d.Inventory(tx, dt.pos).SetItem(i, s)
		}
		*dt.recorder = effectRecorder{}
		d.ScheduledTick(dt.pos, tx, rand.New(rand.NewPCG(3, 4)))
	})
}

// slot returns the stack in a slot of the dispenser.
func (dt *dispenserTest) slot(i int) (s item.Stack) {
	<-dt.w.Exec(func(tx *world.Tx) {
		s, _ = tx.Block(dt.pos).(block.Dispenser).Inventory(tx, dt.pos).Item(i)
	})
	return s
}

// entities returns the entities of the world by their encoded type.
func (dt *dispenserTest) entities(name string) (found []world.Entity) {
	<-dt.w.Exec(func(tx *world.Tx) {
		for e := range tx.Entities() {
			if n := e.H().Type().EncodeEntity(); n == name {
				found = append(found, e)
			}
		}
	})
	return found
}

// soundNames describes the sounds recorded, in order, by type and name.
func (r *effectRecorder) soundNames() (names []string) {
	for _, s := range r.sounds {
		switch s := s.s.(type) {
		case sound.Event:
			name := s.Name
			if s.Block != nil {
				name += "+block"
			}
			names = append(names, name)
		case sound.DispenserLaunch:
			names = append(names, "launch")
		case sound.DispenserFireball:
			names = append(names, "fireball")
		case sound.TNT:
			names = append(names, "fuse")
		default:
			names = append(names, "other")
		}
	}
	return names
}

// smokes returns the particles recorded that are the smoke of a dispenser.
func (r *effectRecorder) smokes() (faces []cube.Face) {
	for _, p := range r.particles {
		if s, ok := p.p.(particle.DispenserSmoke); ok {
			faces = append(faces, s.Face)
		}
	}
	return faces
}

// TestDispenserEffectsFollowBDS checks what a client is shown of a dispense,
// read from BDS 1.26.61.1 (net/bedrock/client/testdata/bds_actors/README.md):
// the click of an empty dispenser at its corner; the smoke and the click of
// an ejected item at the launch point, 0.7 blocks out and 0.2 below the
// centre; the launch event of a projectile, and the click for some.
func TestDispenserEffectsFollowBDS(t *testing.T) {
	t.Parallel()
	launch := mgl64.Vec3{1.2, 64.3, 0.5}
	for _, c := range []struct {
		name   string
		stack  item.Stack
		set    func(tx *world.Tx, front cube.Pos)
		sounds []string
	}{
		{"empty", item.Stack{}, nil, []string{"block.click.fail+block"}},
		{"ejected", item.NewStack(item.Diamond{}, 1), nil, []string{"block.click+block"}},
		{"arrow", item.NewStack(item.Arrow{}, 1), nil, []string{"launch", "block.click"}},
		{"egg", item.NewStack(item.Egg{}, 1), nil, []string{"launch"}},
		{"snowball", item.NewStack(item.Snowball{}, 1), nil, []string{"launch"}},
		{"bottle o' enchanting", item.NewStack(item.BottleOfEnchanting{}, 1), nil, []string{"launch"}},
		{"fire charge", item.NewStack(item.FireCharge{}, 1), nil, []string{"fireball"}},
		{"wind charge", item.NewStack(item.WindCharge{}, 1), nil, []string{"launch", "block.click"}},
		{"firework", item.NewStack(item.Firework{}, 1), nil, []string{"block.click"}},
		{"tnt", item.NewStack(block.TNT{}, 1), nil, []string{"fuse", "block.click"}},
		{"armour stand", item.NewStack(item.ArmourStand{}, 1), nil, []string{"block.click"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dt := newDispenserTest(t, cube.FaceEast)
			dt.dispense(c.set, c.stack)
			if got := dt.recorder.soundNames(); !slices.Equal(got, c.sounds) {
				t.Fatalf("sounds %v, want %v", got, c.sounds)
			}
			if c.name == "empty" {
				if dt.recorder.sounds[0].pos != dt.pos.Vec3() {
					t.Fatalf("empty click at %v, want the corner %v", dt.recorder.sounds[0].pos, dt.pos.Vec3())
				}
				if len(dt.recorder.smokes()) != 0 {
					t.Fatalf("an empty dispenser shows smoke")
				}
				return
			}
			if got := dt.recorder.smokes(); !slices.Equal(got, []cube.Face{cube.FaceEast}) {
				t.Fatalf("smoke towards %v, want east once", got)
			}
			for _, s := range dt.recorder.sounds {
				if s.pos.Sub(launch).Len() > 1e-5 {
					t.Fatalf("sound %T at %v, want the launch point %v", s.s, s.pos, launch)
				}
			}
			for _, p := range dt.recorder.particles {
				if p.pos.Sub(launch).Len() > 1e-5 {
					t.Fatalf("particle %T at %v, want the launch point %v", p.p, p.pos, launch)
				}
			}
		})
	}
}
