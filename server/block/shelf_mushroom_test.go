package block

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// bouncingTestEntity is a non-living entity with a velocity.
type bouncingTestEntity struct {
	pistonTestUser
	h        *world.EntityHandle
	vel      mgl64.Vec3
	sneaking bool
}

func newBouncingTestEntity(vel mgl64.Vec3, sneaking bool) *bouncingTestEntity {
	h := world.EntitySpawnOpts{}.New(dispenserTestEntityType{}, dispenserTestEntityConfig{})
	return &bouncingTestEntity{h: h, vel: vel, sneaking: sneaking}
}

func (e *bouncingTestEntity) H() *world.EntityHandle   { return e.h }
func (e *bouncingTestEntity) Velocity() mgl64.Vec3     { return e.vel }
func (e *bouncingTestEntity) SetVelocity(v mgl64.Vec3) { e.vel = v }
func (e *bouncingTestEntity) Sneaking() bool           { return e.sneaking }

func TestShelfMushroomGrowsAwayFromFirstSupportInLookOrder(t *testing.T) {
	cases := []struct {
		name   string
		yaw    float64
		walls  []cube.Pos
		facing cube.Direction
		placed bool
	}{
		// Looking north at a wall to the north: the mushroom faces back south.
		{"looked at wall", 180, []cube.Pos{{0, 1, -1}}, cube.South, true},
		// Looking north without a wall there falls back to the nearest
		// other horizontal direction with one, east before south.
		{"side wall", 180, []cube.Pos{{1, 1, 0}, {0, 1, 1}}, cube.West, true},
		{"no wall", 180, nil, cube.North, false},
	}
	for _, c := range cases {
		w := newMultifaceTestWorld(t)
		<-w.Exec(func(tx *world.Tx) {
			tx.SetBlock(cube.Pos{0, 0, 0}, Stone{}, nil)
			for _, wall := range c.walls {
				tx.SetBlock(wall, Stone{}, nil)
			}
			// Yaw 180 looks north; ten more degrees tilt the look east.
			user := &pistonTestUser{rot: cube.Rotation{c.yaw + 10, 0}}
			ShelfMushroom{}.UseOnBlock(cube.Pos{0, 0, 0}, cube.FaceUp, mgl64.Vec3{}, tx, user, &item.UseContext{})
			got, placed := tx.Block(cube.Pos{0, 1, 0}).(ShelfMushroom)
			if placed != c.placed || placed && got.Facing != c.facing {
				t.Fatalf("%s: got %#v (placed %v), want facing %v placed %v", c.name, tx.Block(cube.Pos{0, 1, 0}), placed, c.facing, c.placed)
			}
		})
	}
}

func TestShelfMushroomBreaksWithoutSupport(t *testing.T) {
	w := newDropTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 1, -1}, Stone{}, nil)
		tx.SetBlock(cube.Pos{0, 1, 0}, ShelfMushroom{Facing: cube.South}, nil)
		tx.SetBlock(cube.Pos{0, 1, -1}, Air{}, nil)
	})
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		if b := tx.Block(cube.Pos{0, 1, 0}); b != (Air{}) {
			t.Fatalf("unsupported mushroom stayed as %#v", b)
		}
	})
}

func TestShelfMushroomBoneMealGrowsOnceAndDoublesDrops(t *testing.T) {
	w := newMultifaceTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{0, 1, 0}
		tx.SetBlock(cube.Pos{0, 1, -1}, Stone{}, nil)
		tx.SetBlock(pos, ShelfMushroom{Facing: cube.South}, nil)
		if drops := tx.Block(pos).(ShelfMushroom).BreakInfo().Drops(item.ToolNone{}, nil); len(drops) != 1 || drops[0].Count() != 1 {
			t.Fatalf("small mushroom drops %v, want one mushroom", drops)
		}
		if got := tx.Block(pos).(ShelfMushroom).BoneMeal(pos, tx); got == item.BoneMealResultNone {
			t.Fatal("bone meal did not grow the mushroom")
		}
		grown := tx.Block(pos).(ShelfMushroom)
		if grown != (ShelfMushroom{Facing: cube.South, Grown: true}) {
			t.Fatalf("grown mushroom is %#v", grown)
		}
		if got := grown.BoneMeal(pos, tx); got != item.BoneMealResultNone {
			t.Fatal("bone meal grew a grown mushroom")
		}
		if drops := grown.BreakInfo().Drops(item.ToolNone{}, nil); len(drops) != 1 || drops[0].Count() != 2 {
			t.Fatalf("grown mushroom drops %v, want two mushrooms", drops)
		}
	})
}

func TestShelfMushroomHalvesFallAndBouncesUnlessSneaking(t *testing.T) {
	for _, sneaking := range []bool{false, true} {
		w := newMultifaceTestWorld(t)
		<-w.Exec(func(tx *world.Tx) {
			pos := cube.Pos{0, 1, 0}
			m := ShelfMushroom{Facing: cube.South}
			e := newBouncingTestEntity(mgl64.Vec3{0, -1, 0}, sneaking)
			distance := 10.0
			m.EntityLand(pos, tx, e, &distance)
			if distance != 5 {
				t.Fatalf("sneaking %v: fall distance %v, want 5", sneaking, distance)
			}
			want := 0.75 * 0.8
			if sneaking {
				want = -1
			}
			if math.Abs(e.vel[1]-want) > 1e-9 {
				t.Fatalf("sneaking %v: vertical velocity %v, want %v", sneaking, e.vel[1], want)
			}
		})
	}
}

func TestShelfMushroomCapHangsFromItsSupport(t *testing.T) {
	for _, d := range cube.Directions() {
		boxes := ShelfMushroom{Facing: d, Grown: true}.Model().BBox(cube.Pos{}, nil)
		// Every box touches the side of the block the support is on.
		for _, b := range boxes {
			touches := false
			switch d.Opposite().Face() {
			case cube.FaceNorth:
				touches = b.Min()[2] == 0
			case cube.FaceSouth:
				touches = b.Max()[2] == 1
			case cube.FaceWest:
				touches = b.Min()[0] == 0
			case cube.FaceEast:
				touches = b.Max()[0] == 1
			}
			if !touches {
				t.Fatalf("facing %v: box %v does not touch the support side", d, b)
			}
		}
		if len(boxes) != 2 || boxes[0].Width()*boxes[0].Length() != 14.0/16*10.0/16 {
			t.Fatalf("facing %v: grown cap boxes %v", d, boxes)
		}
	}
}
