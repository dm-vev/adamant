package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
)

func newMultifaceTestWorld(t *testing.T) *world.World {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func countLichenFaces(tx *world.Tx, from, to cube.Pos) (n int) {
	for x := from.X(); x <= to.X(); x++ {
		for y := from.Y(); y <= to.Y(); y++ {
			for z := from.Z(); z <= to.Z(); z++ {
				if l, ok := tx.Block(cube.Pos{x, y, z}).(GlowLichen); ok {
					n += l.Faces.Count()
				}
			}
		}
	}
	return n
}

func TestGlowLichenBoneMealGrowsExactlyOneFace(t *testing.T) {
	w := newMultifaceTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		for x := -2; x <= 2; x++ {
			for z := -2; z <= 2; z++ {
				tx.SetBlock(cube.Pos{x, 0, z}, Stone{}, nil)
			}
		}
		pos := cube.Pos{0, 1, 0}
		lichen := GlowLichen{Faces: MultiFace(0).With(cube.FaceDown, true)}
		tx.SetBlock(pos, lichen, nil)
		if res := lichen.BoneMeal(pos, tx); res == item.BoneMealResultNone {
			t.Fatal("lichen on an open floor rejected bone meal")
		}
		if n := countLichenFaces(tx, cube.Pos{-2, 1, -2}, cube.Pos{2, 2, 2}); n != 2 {
			t.Fatalf("lichen faces after one bone meal = %d, want 2", n)
		}
	})
}

func TestGlowLichenEnclosedRejectsBoneMeal(t *testing.T) {
	w := newMultifaceTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{0, 1, 0}
		for _, face := range cube.Faces() {
			tx.SetBlock(pos.Side(face), Stone{}, nil)
		}
		lichen := GlowLichen{Faces: 63}
		tx.SetBlock(pos, lichen, nil)
		if res := lichen.BoneMeal(pos, tx); res != item.BoneMealResultNone {
			t.Fatalf("fully covered, enclosed lichen accepted bone meal: %v", res)
		}
	})
}

func TestMultifaceLosesUnsupportedFaces(t *testing.T) {
	w := newMultifaceTestWorld(t)
	pos := cube.Pos{0, 1, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos.Side(cube.FaceDown), Stone{}, nil)
		tx.SetBlock(pos.Side(cube.FaceNorth), Stone{}, nil)
		tx.SetBlock(pos, SculkVein{Faces: MultiFace(0).With(cube.FaceDown, true).With(cube.FaceNorth, true)}, nil)
	})
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) { tx.SetBlock(pos.Side(cube.FaceNorth), nil, nil) })
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		vein, ok := tx.Block(pos).(SculkVein)
		if !ok || vein.Faces != MultiFace(0).With(cube.FaceDown, true) {
			t.Fatalf("vein after its north support broke = %#v, want only the floor face", tx.Block(pos))
		}
		tx.SetBlock(pos.Side(cube.FaceDown), nil, nil)
	})
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		if _, ok := tx.Block(pos).(Air); !ok {
			t.Fatalf("vein without support = %#v, want air", tx.Block(pos))
		}
	})
}

func TestMultifaceDropsOneItemPerFace(t *testing.T) {
	faces := MultiFace(0).With(cube.FaceDown, true).With(cube.FaceUp, true).With(cube.FaceEast, true)
	silk := []item.Enchantment{item.NewEnchantment(enchantment.SilkTouch, 1)}
	if drops := (SculkVein{Faces: faces}).BreakInfo().Drops(item.ToolNone{}, silk); len(drops) != 1 || drops[0].Count() != 3 {
		t.Fatalf("silk touch vein drops = %v, want 3 veins", drops)
	}
	if drops := (SculkVein{Faces: faces}).BreakInfo().Drops(item.ToolNone{}, nil); len(drops) != 0 {
		t.Fatalf("vein without silk touch drops %v", drops)
	}
	if drops := (GlowLichen{Faces: faces}).BreakInfo().Drops(item.Shears{}, nil); len(drops) != 1 || drops[0].Count() != 3 {
		t.Fatalf("sheared lichen drops = %v, want 3 lichen", drops)
	}
	if drops := (GlowLichen{Faces: faces}).BreakInfo().Drops(item.ToolNone{}, nil); len(drops) != 0 {
		t.Fatalf("lichen broken by hand drops %v", drops)
	}
}

func TestSculkCatalystPersistsChargeCursors(t *testing.T) {
	c := NewSculkCatalyst()
	c.spreader.addCursors(cube.Pos{3, 64, -2}, 1500)
	c.spreader.cursors[1].facings, c.spreader.cursors[1].hasFacings = MultiFace(0).With(cube.FaceDown, true), true

	loaded := (SculkCatalyst{}).DecodeNBT(c.EncodeNBT()).(SculkCatalyst)
	if len(loaded.spreader.cursors) != 2 {
		t.Fatalf("loaded %d cursors, want the 1000 + 500 split", len(loaded.spreader.cursors))
	}
	first, second := loaded.spreader.cursors[0], loaded.spreader.cursors[1]
	if first.charge != 1000 || second.charge != 500 || first.pos != (cube.Pos{3, 64, -2}) {
		t.Fatalf("loaded cursors %+v %+v", *first, *second)
	}
	if first.hasFacings || !second.hasFacings || second.facings != MultiFace(0).With(cube.FaceDown, true) {
		t.Fatalf("facings lost: %+v %+v", *first, *second)
	}
}
