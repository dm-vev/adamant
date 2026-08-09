package block

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestBubbleColumnFormationDirectionAndRemoval(t *testing.T) {
	w := newBubbleTestWorld(t)
	base := cube.Pos{0, 64, 0}

	w.Do(func(tx *world.Tx) {
		for y := 1; y <= 3; y++ {
			tx.SetLiquid(base.Add(cube.Pos{0, y, 0}), Water{Depth: 8, Still: true})
		}
		tx.SetBlock(base, Magma{}, nil)
	})
	w.AdvanceTick()
	assertBubbleColumn(t, w, base, 3, true)

	w.Do(func(tx *world.Tx) { tx.SetBlock(base, SoulSand{}, nil) })
	w.AdvanceTick()
	assertBubbleColumn(t, w, base, 3, false)

	w.Do(func(tx *world.Tx) { tx.SetBlock(base, Stone{}, nil) })
	w.AdvanceTick()
	w.Do(func(tx *world.Tx) {
		for y := 1; y <= 3; y++ {
			pos := base.Add(cube.Pos{0, y, 0})
			water, ok := tx.Block(pos).(Water)
			if !ok || !isSourceWater(water) {
				t.Fatalf("block at %v after removal = %#v, want source water", pos, tx.Block(pos))
			}
		}
	})
}

func TestBubbleColumnInterruptedAndRestored(t *testing.T) {
	w := newBubbleTestWorld(t)
	base := cube.Pos{0, 64, 0}
	w.Do(func(tx *world.Tx) {
		tx.SetBlock(base, SoulSand{}, nil)
		for y := 1; y <= 3; y++ {
			tx.SetLiquid(base.Add(cube.Pos{0, y, 0}), Water{Depth: 8, Still: true})
		}
	})
	w.AdvanceTick()

	middle := base.Add(cube.Pos{0, 2, 0})
	w.Do(func(tx *world.Tx) { tx.SetLiquid(middle, nil) })
	w.AdvanceTick()
	w.Do(func(tx *world.Tx) {
		if _, ok := tx.Block(base.Side(cube.FaceUp)).(BubbleColumn); !ok {
			t.Fatal("bubble column below interruption was removed")
		}
		if _, ok := tx.Block(middle).(Air); !ok {
			t.Fatalf("interrupted block = %#v, want air", tx.Block(middle))
		}
		if water, ok := tx.Block(middle.Side(cube.FaceUp)).(Water); !ok || !isSourceWater(water) {
			t.Fatalf("water above interruption = %#v, want source water", tx.Block(middle.Side(cube.FaceUp)))
		}
	})

	w.Do(func(tx *world.Tx) { tx.SetLiquid(middle, Water{Depth: 8, Still: true}) })
	w.AdvanceTick()
	assertBubbleColumn(t, w, base, 3, false)
}

func TestBubbleColumnsRequireSourceWater(t *testing.T) {
	tests := []struct {
		name  string
		water Water
		forms bool
	}{
		{name: "source", water: Water{Depth: 8, Still: true}, forms: true},
		{name: "flowing", water: Water{Depth: 7}, forms: false},
		{name: "falling", water: Water{Depth: 8, Falling: true}, forms: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := newBubbleTestWorld(t)
			base := cube.Pos{0, 64, 0}
			w.Do(func(tx *world.Tx) {
				tx.SetLiquid(base.Side(cube.FaceUp), test.water)
				tx.SetBlock(base, SoulSand{}, nil)
			})
			w.AdvanceTick()
			w.Do(func(tx *world.Tx) {
				_, formed := tx.Block(base.Side(cube.FaceUp)).(BubbleColumn)
				if formed != test.forms {
					t.Fatalf("bubble column formed = %t, want %t", formed, test.forms)
				}
			})
		})
	}
}

func TestBubbleColumnWorldHeightBoundary(t *testing.T) {
	w := newBubbleTestWorld(t)
	var base cube.Pos
	w.Do(func(tx *world.Tx) {
		base = cube.Pos{0, tx.Range()[1] - 1, 0}
		tx.SetLiquid(base.Side(cube.FaceUp), Water{Depth: 8, Still: true})
		tx.SetBlock(base, Magma{}, nil)
	})
	w.AdvanceTick()
	assertBubbleColumn(t, w, base, 1, true)
}

func TestBubbleColumnEntityMotion(t *testing.T) {
	tests := []struct {
		name     string
		column   BubbleColumn
		surface  bool
		velocity float64
		want     float64
	}{
		{name: "rise", velocity: 0, want: 0.06},
		{name: "rise limit", velocity: 0.69, want: 0.70},
		{name: "surface rise", surface: true, velocity: 1.75, want: 1.80},
		{name: "sink", column: BubbleColumn{DragDown: true}, velocity: 0, want: -0.03},
		{name: "sink limit", column: BubbleColumn{DragDown: true}, velocity: -0.29, want: -0.30},
		{name: "surface sink", column: BubbleColumn{DragDown: true}, surface: true, velocity: -0.88, want: -0.90},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := newBubbleTestWorld(t)
			pos := cube.Pos{0, 64, 0}
			w.Do(func(tx *world.Tx) {
				_ = tx.Block(pos)
				if !test.surface {
					tx.SetBlock(pos.Side(cube.FaceUp), BubbleColumn{}, nil)
				}
				e := &bubbleTestEntity{vel: mgl64.Vec3{0.2, test.velocity, -0.1}}
				test.column.EntityInside(pos, tx, e)
				if math.Abs(e.vel[1]-test.want) > 1e-9 {
					t.Fatalf("vertical velocity = %v, want %v", e.vel[1], test.want)
				}
				if e.vel[0] != 0.2 || e.vel[2] != -0.1 {
					t.Fatalf("horizontal velocity changed to %v", e.vel)
				}
				if !e.fallReset {
					t.Fatal("fall distance was not reset")
				}
			})
		})
	}
}

func TestBubbleColumnRegistration(t *testing.T) {
	registry := world.DefaultBlockRegistry
	registry.Finalize()
	for _, dragDown := range []bool{false, true} {
		b, ok := registry.BlockByName("minecraft:bubble_column", map[string]any{"drag_down": dragDown})
		column, columnOK := b.(BubbleColumn)
		if !ok || !columnOK || column.DragDown != dragDown {
			t.Fatalf("BlockByName(drag_down=%t) = %#v, %t", dragDown, b, ok)
		}
		rid := registry.BlockRuntimeID(column)
		decoded, ok := registry.BlockByRuntimeID(rid)
		if !ok || decoded != column {
			t.Fatalf("runtime ID %d decoded to %#v, %t; want %#v", rid, decoded, ok, column)
		}
		if registry.NBTBlock(rid) {
			t.Fatalf("bubble column runtime ID %d incorrectly marked as block NBT", rid)
		}
	}
}

func TestLavaHardensNextToWaterloggedBubbleColumn(t *testing.T) {
	w := newBubbleTestWorld(t)
	lavaPos, bubblePos := cube.Pos{0, 64, 0}, cube.Pos{1, 64, 0}
	w.Do(func(tx *world.Tx) {
		tx.SetBlock(bubblePos, BubbleColumn{}, nil)
		tx.SetLiquid(bubblePos, Water{Depth: 8, Still: true})
		lava := Lava{Depth: 8, Still: true}
		tx.SetBlock(lavaPos, lava, nil)
		if !lava.Harden(lavaPos, tx, nil) {
			t.Fatal("lava did not harden next to a waterlogged bubble column")
		}
		if _, ok := tx.Block(lavaPos).(Obsidian); !ok {
			t.Fatalf("hardened lava = %#v, want obsidian", tx.Block(lavaPos))
		}
	})
}

func newBubbleTestWorld(t *testing.T) *world.World {
	t.Helper()
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func assertBubbleColumn(t *testing.T, w *world.World, base cube.Pos, height int, dragDown bool) {
	t.Helper()
	w.Do(func(tx *world.Tx) {
		for y := 1; y <= height; y++ {
			pos := base.Add(cube.Pos{0, y, 0})
			column, ok := tx.Block(pos).(BubbleColumn)
			if !ok || column.DragDown != dragDown {
				t.Fatalf("block at %v = %#v, want BubbleColumn{DragDown:%t}", pos, tx.Block(pos), dragDown)
			}
			liquid, ok := tx.Liquid(pos)
			if !ok || !isSourceWater(liquid) {
				t.Fatalf("liquid at %v = %#v, %t; want source water", pos, liquid, ok)
			}
		}
	})
}

type bubbleTestEntity struct {
	vel       mgl64.Vec3
	fallReset bool
}

func (*bubbleTestEntity) Close() error               { return nil }
func (*bubbleTestEntity) H() *world.EntityHandle     { return nil }
func (*bubbleTestEntity) Position() mgl64.Vec3       { return mgl64.Vec3{} }
func (*bubbleTestEntity) Rotation() cube.Rotation    { return cube.Rotation{} }
func (e *bubbleTestEntity) Velocity() mgl64.Vec3     { return e.vel }
func (e *bubbleTestEntity) SetVelocity(v mgl64.Vec3) { e.vel = v }
func (e *bubbleTestEntity) ResetFallDistance()       { e.fallReset = true }
func (*bubbleTestEntity) FallDistance() float64      { return 0 }
