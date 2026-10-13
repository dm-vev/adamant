package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// standingTestEntity is an entity standing on the ground at a position.
type standingTestEntity struct {
	pistonTestUser
	onGround bool
}

func (e *standingTestEntity) OnGround() bool { return e.onGround }

func dripleafAt(t *testing.T, w *world.World, pos cube.Pos) (b world.Block) {
	t.Helper()
	<-w.Exec(func(tx *world.Tx) { b = tx.Block(pos) })
	return b
}

func TestBigDripleafTiltsUnderStandingEntityAndRecovers(t *testing.T) {
	w := newMultifaceTestWorld(t)
	leaf := cube.Pos{0, 1, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 0, 0}, Clay{}, nil)
		tx.SetBlock(leaf, BigDripleaf{Head: true, Facing: cube.North}, nil)
		standing := &standingTestEntity{pistonTestUser{pos: mgl64.Vec3{0.5, 1.9375, 0.5}}, true}
		tx.Block(leaf).(EntityInsider).EntityInside(leaf, tx, standing)
	})
	steps := []struct {
		ticks int
		tilt  DripleafTilt
	}{
		{0, DripleafTiltUnstable},
		{9, DripleafTiltUnstable}, {1, DripleafTiltPartial},
		{9, DripleafTiltPartial}, {1, DripleafTiltFull},
		{99, DripleafTiltFull}, {1, DripleafTiltNone},
	}
	elapsed := 0
	for _, s := range steps {
		for range s.ticks {
			w.AdvanceTick()
		}
		elapsed += s.ticks
		if got := dripleafAt(t, w, leaf); got != (BigDripleaf{Head: true, Facing: cube.North, Tilt: s.tilt}) {
			t.Fatalf("after %d ticks: %#v, want tilt %v", elapsed, got, s.tilt)
		}
	}
}

func TestBigDripleafIgnoresAirborneOrLowEntities(t *testing.T) {
	cases := []struct {
		name     string
		y        float64
		onGround bool
	}{
		{"airborne above the leaf", 1.9375, false},
		{"standing below the leaf top", 1.5, true},
	}
	for _, c := range cases {
		w := newMultifaceTestWorld(t)
		leaf := cube.Pos{0, 1, 0}
		<-w.Exec(func(tx *world.Tx) {
			tx.SetBlock(cube.Pos{0, 0, 0}, Clay{}, nil)
			tx.SetBlock(leaf, BigDripleaf{Head: true}, nil)
			e := &standingTestEntity{pistonTestUser{pos: mgl64.Vec3{0.5, c.y, 0.5}}, c.onGround}
			tx.Block(leaf).(EntityInsider).EntityInside(leaf, tx, e)
		})
		if got := dripleafAt(t, w, leaf); got != (BigDripleaf{Head: true}) {
			t.Fatalf("%s: leaf became %#v", c.name, got)
		}
	}
}

func TestPoweredBigDripleafStaysLevel(t *testing.T) {
	w := newMultifaceTestWorld(t)
	leaf := cube.Pos{0, 1, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 0, 0}, Clay{}, nil)
		tx.SetBlock(leaf, BigDripleaf{Head: true, Tilt: DripleafTiltPartial}, nil)
		tx.SetBlock(cube.Pos{1, 1, 0}, RedstoneBlock{}, nil)
	})
	w.AdvanceTick()
	if got := dripleafAt(t, w, leaf); got != (BigDripleaf{Head: true}) {
		t.Fatalf("powered tipped leaf stayed %#v, want level", got)
	}
	<-w.Exec(func(tx *world.Tx) {
		e := &standingTestEntity{pistonTestUser{pos: mgl64.Vec3{0.5, 1.9375, 0.5}}, true}
		tx.Block(leaf).(EntityInsider).EntityInside(leaf, tx, e)
	})
	if got := dripleafAt(t, w, leaf); got != (BigDripleaf{Head: true}) {
		t.Fatalf("powered leaf tipped to %#v", got)
	}
}

func TestBigDripleafLeafBecomesStemUnderNewLeaf(t *testing.T) {
	w := newMultifaceTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 0, 0}, Clay{}, nil)
		tx.SetBlock(cube.Pos{0, 1, 0}, BigDripleaf{Head: true, Facing: cube.East}, nil)
		BigDripleaf{Head: true}.UseOnBlock(cube.Pos{0, 1, 0}, cube.FaceUp, mgl64.Vec3{}, tx, &pistonTestUser{rot: cube.Rotation{90, 0}}, &item.UseContext{})
	})
	w.AdvanceTick()
	if got := dripleafAt(t, w, cube.Pos{0, 1, 0}); got != (BigDripleaf{Facing: cube.East}) {
		t.Fatalf("covered leaf became %#v, want an east stem", got)
	}
	if got := dripleafAt(t, w, cube.Pos{0, 2, 0}); got != (BigDripleaf{Head: true, Facing: cube.East}) {
		t.Fatalf("placed leaf is %#v, want the facing of the plant below", got)
	}
}

func TestBoneMealGrowsBigDripleafFromStemAndSmallDripleaf(t *testing.T) {
	w := newMultifaceTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 0, 0}, Clay{}, nil)
		tx.SetBlock(cube.Pos{0, 1, 0}, BigDripleaf{Facing: cube.South}, nil)
		tx.SetBlock(cube.Pos{0, 2, 0}, BigDripleaf{Head: true, Facing: cube.South}, nil)
		if res := (BigDripleaf{Facing: cube.South}).BoneMeal(cube.Pos{0, 1, 0}, tx); res == item.BoneMealResultNone {
			t.Fatalf("bone meal on a stem failed")
		}
		for y, want := range []BigDripleaf{{Facing: cube.South}, {Facing: cube.South}, {Head: true, Facing: cube.South}} {
			if got := tx.Block(cube.Pos{0, y + 1, 0}); got != want {
				t.Fatalf("y=%d: %#v, want %#v", y+1, got, want)
			}
		}

		tx.SetBlock(cube.Pos{4, 0, 0}, Clay{}, nil)
		tx.SetBlock(cube.Pos{4, 1, 0}, SmallDripleaf{Facing: cube.West}, nil)
		tx.SetBlock(cube.Pos{4, 2, 0}, SmallDripleaf{Facing: cube.West, UpperPart: true}, nil)
		(SmallDripleaf{Facing: cube.West, UpperPart: true}).BoneMeal(cube.Pos{4, 2, 0}, tx)
		height := 0
		for y := 1; ; y++ {
			d, ok := tx.Block(cube.Pos{4, y, 0}).(BigDripleaf)
			if !ok {
				break
			}
			if d.Facing != cube.West {
				t.Fatalf("grown part faces %v, want west", d.Facing)
			}
			height++
			if d.Head {
				break
			}
		}
		if height < 2 || height > 5 {
			t.Fatalf("small dripleaf grew a big dripleaf %d tall, want 2 to 5", height)
		}
	})
}

func TestSmallDripleafPlacementNeedsClayOrWaterOverSoil(t *testing.T) {
	cases := []struct {
		name   string
		ground world.Block
		water  bool
		placed bool
	}{
		{"clay without water", Clay{}, false, true},
		{"dirt without water", Dirt{}, false, false},
		{"dirt under water", Dirt{}, true, true},
		{"stone under water", Stone{}, true, false},
	}
	for _, c := range cases {
		w := newMultifaceTestWorld(t)
		<-w.Exec(func(tx *world.Tx) {
			tx.SetBlock(cube.Pos{0, 0, 0}, c.ground, nil)
			if c.water {
				tx.SetBlock(cube.Pos{0, 1, 0}, Water{Depth: 8, Still: true}, nil)
				tx.SetBlock(cube.Pos{0, 2, 0}, Water{Depth: 8, Still: true}, nil)
			}
			SmallDripleaf{}.UseOnBlock(cube.Pos{0, 0, 0}, cube.FaceUp, mgl64.Vec3{}, tx, &pistonTestUser{}, &item.UseContext{})
			_, lower := tx.Block(cube.Pos{0, 1, 0}).(SmallDripleaf)
			upper, _ := tx.Block(cube.Pos{0, 2, 0}).(SmallDripleaf)
			if lower != c.placed || upper.UpperPart != c.placed {
				t.Fatalf("%s: placed lower=%v upper=%v, want %v", c.name, lower, upper.UpperPart, c.placed)
			}
		})
	}
}
