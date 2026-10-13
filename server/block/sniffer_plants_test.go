package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestTorchflowerCropBloomsAfterTwoStages(t *testing.T) {
	w := newMultifaceTestWorld(t)
	pos := cube.Pos{0, 1, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 0, 0}, Farmland{}, nil)
		TorchflowerCrop{}.UseOnBlock(cube.Pos{0, 0, 0}, cube.FaceUp, mgl64.Vec3{}, tx, &pistonTestUser{}, &item.UseContext{})
		for _, want := range []world.Block{TorchflowerCrop{Growth: 1}, Torchflower{}} {
			tx.Block(pos).(item.BoneMealAffected).BoneMeal(pos, tx)
			if got := tx.Block(pos); got != want {
				t.Fatalf("after bone meal: %#v, want %#v", got, want)
			}
		}
	})
}

func TestTorchflowerSeedsNeedFarmland(t *testing.T) {
	w := newMultifaceTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 0, 0}, Dirt{}, nil)
		TorchflowerCrop{}.UseOnBlock(cube.Pos{0, 0, 0}, cube.FaceUp, mgl64.Vec3{}, tx, &pistonTestUser{}, &item.UseContext{})
		if got := tx.Block(cube.Pos{0, 1, 0}); got != (Air{}) {
			t.Fatalf("seeds planted on dirt: %#v", got)
		}
	})
}

func TestPitcherCropGrowsIntoTwoBlocks(t *testing.T) {
	w := newMultifaceTestWorld(t)
	lower, upper := cube.Pos{0, 1, 0}, cube.Pos{0, 2, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 0, 0}, Farmland{}, nil)
		PitcherCrop{}.UseOnBlock(cube.Pos{0, 0, 0}, cube.FaceUp, mgl64.Vec3{}, tx, &pistonTestUser{}, &item.UseContext{})
		for growth := 1; growth <= 4; growth++ {
			if res := tx.Block(lower).(PitcherCrop).BoneMeal(lower, tx); res == item.BoneMealResultNone {
				t.Fatalf("bone meal failed below growth %d", growth)
			}
			if got := tx.Block(lower); got != (PitcherCrop{Growth: growth}) {
				t.Fatalf("lower half %#v, want growth %d", got, growth)
			}
			var want world.Block = Air{}
			if growth >= 3 {
				want = PitcherCrop{Growth: growth, UpperPart: true}
			}
			if got := tx.Block(upper); got != want {
				t.Fatalf("growth %d: upper %#v, want %#v", growth, got, want)
			}
		}
		if res := (PitcherCrop{Growth: 4, UpperPart: true}).BoneMeal(upper, tx); res != item.BoneMealResultNone {
			t.Fatalf("fully grown crop took bone meal")
		}
	})
}

func TestPitcherCropNeedsRoomToGrowTall(t *testing.T) {
	w := newMultifaceTestWorld(t)
	lower := cube.Pos{0, 1, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 0, 0}, Farmland{}, nil)
		tx.SetBlock(lower, PitcherCrop{Growth: 2}, nil)
		tx.SetBlock(cube.Pos{0, 2, 0}, Stone{}, nil)
		if res := (PitcherCrop{Growth: 2}).BoneMeal(lower, tx); res != item.BoneMealResultNone || tx.Block(lower) != (PitcherCrop{Growth: 2}) {
			t.Fatalf("crop under stone grew to %#v", tx.Block(lower))
		}
	})
}

func TestPitcherCropDrops(t *testing.T) {
	for growth, want := range []world.Item{PitcherCrop{}, PitcherCrop{}, PitcherCrop{}, PitcherCrop{}, PitcherPlant{}} {
		drops := PitcherCrop{Growth: growth}.BreakInfo().Drops(item.ToolNone{}, nil)
		if len(drops) != 1 || drops[0].Item() != want {
			t.Fatalf("growth %d dropped %v, want %T", growth, drops, want)
		}
	}
}

func TestScaffoldingDistanceFromSupportedColumn(t *testing.T) {
	w := newMultifaceTestWorld(t)
	user := &pistonTestUser{rot: cube.Rotation{-90, 0}}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 0, 0}, Stone{}, nil)
		Scaffolding{}.UseOnBlock(cube.Pos{0, 0, 0}, cube.FaceUp, mgl64.Vec3{}, tx, user, &item.UseContext{})
		// Used on the side of scaffolding, it stacks upward.
		Scaffolding{}.UseOnBlock(cube.Pos{0, 1, 0}, cube.FaceNorth, mgl64.Vec3{}, tx, user, &item.UseContext{})
		if tx.Block(cube.Pos{0, 1, 0}) != (Scaffolding{}) || tx.Block(cube.Pos{0, 2, 0}) != (Scaffolding{}) {
			t.Fatalf("column: %#v %#v", tx.Block(cube.Pos{0, 1, 0}), tx.Block(cube.Pos{0, 2, 0}))
		}
		// Used on its top, it extends toward the user's facing, each block a
		// step further from the supported column.
		facing := user.rot.Direction().Face()
		for i := 1; i <= 7; i++ {
			Scaffolding{}.UseOnBlock(cube.Pos{0, 2, 0}, cube.FaceUp, mgl64.Vec3{}, tx, user, &item.UseContext{})
			if got := tx.Block(cube.Pos{0, 2, 0}.Add(offsetOf(facing, i))); got != (Scaffolding{Distance: i}) {
				t.Fatalf("bridge block %d is %#v", i, got)
			}
		}
		// Bridges reach no further than seven blocks.
		Scaffolding{}.UseOnBlock(cube.Pos{0, 2, 0}, cube.FaceUp, mgl64.Vec3{}, tx, user, &item.UseContext{})
		if got := tx.Block(cube.Pos{0, 2, 0}.Add(offsetOf(facing, 8))); got != (Air{}) {
			t.Fatalf("scaffolding extended eight blocks: %#v", got)
		}
	})
}

// offsetOf returns n blocks toward face.
func offsetOf(face cube.Face, n int) cube.Pos {
	o := cube.Pos{}.Side(face)
	return cube.Pos{o[0] * n, o[1] * n, o[2] * n}
}
