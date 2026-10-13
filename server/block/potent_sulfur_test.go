package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

var sulfurPos = cube.Pos{0, 1, 0}

// buildSulfurPool walls in a column of water sources of the depth passed over
// sulfurPos, with the block passed below it.
func buildSulfurPool(tx *world.Tx, below world.Block, depth int) {
	tx.SetBlock(sulfurPos.Side(cube.FaceDown), below, nil)
	for y := 1; y <= depth+1; y++ {
		for _, f := range cube.HorizontalFaces() {
			tx.SetBlock(sulfurPos.Add(cube.Pos{0, y, 0}).Side(f), Stone{}, nil)
		}
	}
	for y := 1; y <= depth; y++ {
		tx.SetBlock(sulfurPos.Add(cube.Pos{0, y, 0}), Water{Depth: 8, Still: true}, nil)
	}
}

func sulfurState(t *testing.T, w *world.World) (s PotentSulfur) {
	t.Helper()
	<-w.Exec(func(tx *world.Tx) { s = tx.Block(sulfurPos).(PotentSulfur) })
	return s
}

func TestPotentSulfurPlacementStateFollowsWaterAndHeat(t *testing.T) {
	cases := []struct {
		name  string
		below world.Block
		water int
		want  PotentSulfurState
	}{
		{"dry", Stone{}, 0, PotentSulfurDry},
		{"under water", Stone{}, 1, PotentSulfurWet},
		{"over magma", Magma{}, 2, PotentSulfurDormant},
		{"over lava source", Lava{Depth: 8, Still: true}, 1, PotentSulfurContinuous},
		{"over flowing lava", Lava{Depth: 6, Still: true}, 1, PotentSulfurWet},
		{"over magma without water", Magma{}, 0, PotentSulfurDry},
	}
	for _, c := range cases {
		w := newMultifaceTestWorld(t)
		<-w.Exec(func(tx *world.Tx) {
			buildSulfurPool(tx, c.below, c.water)
			tx.SetBlock(sulfurPos, Air{}, nil)
			// Clicking the free spot itself: lava below would be replaced.
			NewPotentSulfur().UseOnBlock(sulfurPos, cube.FaceUp, mgl64.Vec3{}, tx, &pistonTestUser{}, &item.UseContext{})
		})
		if got := sulfurState(t, w).State; got != c.want {
			t.Errorf("%s: state %v, want %v", c.name, got, c.want)
		}
	}
}

func TestPotentSulfurStateUpdatesWithNeighbours(t *testing.T) {
	w := newMultifaceTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		buildSulfurPool(tx, Stone{}, 0)
		tx.SetBlock(sulfurPos, NewPotentSulfur(), nil)
	})
	steps := []struct {
		name   string
		change func(tx *world.Tx)
		want   PotentSulfurState
	}{
		{"water poured", func(tx *world.Tx) { tx.SetBlock(sulfurPos.Side(cube.FaceUp), Water{Depth: 8, Still: true}, nil) }, PotentSulfurWet},
		{"magma below", func(tx *world.Tx) { tx.SetBlock(sulfurPos.Side(cube.FaceDown), Magma{}, nil) }, PotentSulfurDormant},
		{"lava below", func(tx *world.Tx) { tx.SetBlock(sulfurPos.Side(cube.FaceDown), Lava{Depth: 8, Still: true}, nil) }, PotentSulfurContinuous},
		{"water drained", func(tx *world.Tx) { tx.SetBlock(sulfurPos.Side(cube.FaceUp), Air{}, nil) }, PotentSulfurDry},
	}
	for _, s := range steps {
		<-w.Exec(s.change)
		w.AdvanceTick()
		if got := sulfurState(t, w).State; got != s.want {
			t.Fatalf("after %s: state %v, want %v", s.name, got, s.want)
		}
	}
}

// geyserTransitions advances the world for the seconds passed and returns
// the seconds, counted from the start, at which the state changed.
func geyserTransitions(t *testing.T, w *world.World, seconds int) (at []int, states []PotentSulfurState) {
	t.Helper()
	prev := sulfurState(t, w).State
	for tick := 1; tick <= seconds*20; tick++ {
		w.AdvanceTick()
		if s := sulfurState(t, w).State; s != prev {
			at, states = append(at, (tick+19)/20), append(states, s)
			prev = s
		}
	}
	return at, states
}

func TestPeriodicGeyserRepeatsPhasesFixedByPosition(t *testing.T) {
	for depth := 1; depth <= 4; depth++ {
		w := newMultifaceTestWorld(t)
		<-w.Exec(func(tx *world.Tx) {
			buildSulfurPool(tx, Magma{}, depth)
			tx.SetBlock(sulfurPos, NewPotentSulfur().withSurroundings(sulfurPos, tx), nil)
		})
		at, states := geyserTransitions(t, w, 2*(60+5)+5)
		if len(at) < 4 {
			t.Fatalf("depth %d: transitions at %v, want two full cycles", depth, at)
		}
		for i, s := range states[:4] {
			want := PotentSulfurErupting
			if i%2 == 1 {
				want = PotentSulfurDormant
			}
			if s != want {
				t.Fatalf("depth %d: transition %d to %v, want %v", depth, i, s, want)
			}
		}
		erupt, dormant, erupt2 := at[1]-at[0], at[2]-at[1], at[3]-at[2]
		// Wiki: eruptions last (depth-1)+1..2 seconds, dormancy
		// 10*(depth-1)+15..30 seconds.
		if erupt < depth || erupt > depth+1 || erupt2 != erupt {
			t.Fatalf("depth %d: eruptions of %d and %d seconds", depth, erupt, erupt2)
		}
		if dormant < 10*(depth-1)+15 || dormant > 10*(depth-1)+30 {
			t.Fatalf("depth %d: dormant for %d seconds", depth, dormant)
		}
		// The first dormant phase starts counting on the first second.
		if at[0] != dormant {
			t.Fatalf("depth %d: first eruption after %d seconds, later dormancy %d", depth, at[0], dormant)
		}
	}
}

func TestGeyserCountdownLoadsFromNBT(t *testing.T) {
	w := newMultifaceTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		buildSulfurPool(tx, Magma{}, 1)
		loaded := PotentSulfur{State: PotentSulfurDormant}.DecodeNBT(map[string]any{"id": "PotentSulfurBlock", "countdown": int32(3)}).(PotentSulfur)
		tx.SetBlock(sulfurPos, loaded, nil)
	})
	at, states := geyserTransitions(t, w, 5)
	if len(at) == 0 || at[0] != 3 || states[0] != PotentSulfurErupting {
		t.Fatalf("transitions %v at %v, want eruption after 3 seconds", states, at)
	}
}

func TestPotentSulfurWithoutGasSourcePausesCountdown(t *testing.T) {
	w := newMultifaceTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		buildSulfurPool(tx, Magma{}, 1)
		tx.SetBlock(sulfurPos, PotentSulfur{State: PotentSulfurDormant, Countdown: 2}, nil)
		// A solid block on the water leaves no gas source.
		tx.SetBlock(sulfurPos.Add(cube.Pos{0, 2, 0}), Stone{}, nil)
	})
	if at, _ := geyserTransitions(t, w, 10); len(at) != 0 {
		t.Fatalf("covered geyser changed state at %v", at)
	}
	if got := sulfurState(t, w).Countdown; got != 2 {
		t.Fatalf("covered geyser countdown %d, want 2", got)
	}
}
