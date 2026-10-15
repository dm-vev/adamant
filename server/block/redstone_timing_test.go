package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// redstoneTimingWorld returns a scheduler-driven world with a stone floor
// below y=64 around the origin.
func redstoneTimingWorld(t *testing.T) *world.World {
	t.Helper()
	w := world.Config{Dim: world.End, Synchronous: true}.New()
	loader := world.NewLoader(2, w, world.NopViewer{})
	t.Cleanup(func() {
		runWorld(w, func(tx *world.Tx) { loader.Close(tx) })
		_ = w.Close()
	})
	runWorld(w, func(tx *world.Tx) {
		loader.Move(tx, mgl64.Vec3{0, 64, 0})
		loader.Load(tx, 16)
		for x := -2; x <= 8; x++ {
			for z := -2; z <= 2; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, Stone{}, nil)
			}
		}
	})
	return w
}

// ticksUntil advances the world until done holds and returns how many ticks
// that took, or -1 after limit ticks.
func ticksUntil(w *world.World, limit int, done func(tx *world.Tx) bool) int {
	for n := 1; n <= limit; n++ {
		w.AdvanceTick()
		var ok bool
		runWorld(w, func(tx *world.Tx) { ok = done(tx) })
		if ok {
			return n
		}
	}
	return -1
}

// TestRedstoneDelaysMatchJava checks the delays the components schedule in
// game ticks, as Java and BDS run them: a repeater waits two ticks per delay
// step, a lamp goes out four ticks after losing power and buttons stay
// pressed 20 (stone) or 30 (wood) ticks. Delays are measured inside a
// repeater chain, from the first repeater switching.
func TestRedstoneDelaysMatchJava(t *testing.T) {
	for delay := range 4 {
		w := redstoneTimingWorld(t)
		source, first, second, lamp := cube.Pos{0, 64, 0}, cube.Pos{1, 64, 0}, cube.Pos{2, 64, 0}, cube.Pos{3, 64, 0}
		runWorld(w, func(tx *world.Tx) {
			tx.SetBlock(first, RedstoneRepeater{Facing: cube.West}, nil)
			tx.SetBlock(second, RedstoneRepeater{Facing: cube.West, Delay: delay}, nil)
			tx.SetBlock(lamp, RedstoneLamp{}, nil)
		})
		w.AdvanceTick()
		runWorld(w, func(tx *world.Tx) { tx.SetBlock(source, RedstoneBlock{}, nil) })
		firstOn := ticksUntil(w, 40, func(tx *world.Tx) bool { return tx.Block(first).(RedstoneRepeater).Powered })
		lampOn := ticksUntil(w, 40, func(tx *world.Tx) bool { return tx.Block(lamp).(RedstoneLamp).Lit })
		if firstOn < 0 || lampOn != 2*(delay+1) {
			t.Fatalf("delay %d repeater lit the lamp %d ticks after its input switched, want %d", delay+1, lampOn, 2*(delay+1))
		}
		runWorld(w, func(tx *world.Tx) { tx.SetBlock(source, Air{}, nil) })
		firstOff := ticksUntil(w, 40, func(tx *world.Tx) bool { return !tx.Block(first).(RedstoneRepeater).Powered })
		secondOff := ticksUntil(w, 40, func(tx *world.Tx) bool { return !tx.Block(second).(RedstoneRepeater).Powered })
		lampOff := ticksUntil(w, 40, func(tx *world.Tx) bool { return !tx.Block(lamp).(RedstoneLamp).Lit })
		if firstOff < 0 || secondOff != 2*(delay+1) || lampOff != 4 {
			t.Fatalf("delay %d repeater turned off %d ticks after its input and the lamp %d ticks later, want %d and 4", delay+1, secondOff, lampOff, 2*(delay+1))
		}
	}

	for _, test := range []struct {
		typ  ButtonType
		want int
	}{{StoneButton(), 20}, {WoodButton(), 30}} {
		w := redstoneTimingWorld(t)
		pos := cube.Pos{0, 64, 0}
		runWorld(w, func(tx *world.Tx) {
			tx.SetBlock(pos, Button{Type: test.typ, Facing: cube.FaceUp}, nil)
			tx.Block(pos).(Button).Activate(pos, cube.FaceUp, tx, nil, nil)
		})
		released := ticksUntil(w, 80, func(tx *world.Tx) bool { return !tx.Block(pos).(Button).Pressed })
		if released != test.want {
			t.Fatalf("%v button released after %d ticks, want %d", test.typ, released, test.want)
		}
	}
}
