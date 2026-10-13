package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// wall builds a stone wall at z=0 spanning x in [x0,x1] and y in [y0,y1]; the
// paintings hang in the air at z=-1 facing north.
func wall(tx *world.Tx, x0, x1, y0, y1 int) {
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			tx.SetBlock(cube.Pos{x, y, 0}, block.Stone{}, nil)
		}
	}
}

func TestPaintingChoosesLargestFittingCanvas(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		// A 2x2 wall: Java offsets even canvases left (west when facing north) and up.
		wall(tx, -1, 0, 64, 65)
		for range 20 {
			h := CreatePainting(tx, cube.Pos{0, 64, -1}, cube.North)
			if h == nil {
				t.Fatal("no canvas fits a 2x2 wall")
			}
			p := tx.AddEntity(h).(*Painting)
			if got := p.Motive(); got.Width != 2 || got.Height != 2 || !got.placeable() {
				t.Fatalf("2x2 wall chose %+v", got)
			}
			_ = p.CloseIn(tx)
		}
		// Anchored at the right edge, a 2-wide canvas would overhang open air.
		if h := CreatePainting(tx, cube.Pos{-1, 64, -1}, cube.North); h != nil {
			p := tx.AddEntity(h).(*Painting)
			if got := p.Motive(); got.Width*got.Height > 2 || got.Width > 1 {
				t.Fatalf("canvas %+v overhangs the wall edge", got)
			}
			_ = p.CloseIn(tx)
		}
	})
}

func TestPaintingRejectsOverlapAndBlockedSpace(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		wall(tx, 0, 0, 64, 64)
		if CreatePainting(tx, cube.Pos{0, 64, 0}, cube.North) != nil {
			t.Fatal("painting placed inside a solid block")
		}
		tx.AddEntity(NewPainting(world.EntitySpawnOpts{}, cube.Pos{0, 64, -1}, cube.North, PaintingMotives[0]))
		if CreatePainting(tx, cube.Pos{0, 64, -1}, cube.North) != nil {
			t.Fatal("painting placed over an existing painting")
		}
		if CreatePainting(tx, cube.Pos{1, 64, -1}, cube.North) != nil {
			t.Fatal("painting placed without a wall behind it")
		}
	})
}

func TestPaintingDropsAfterSupportCheckAndOnAttack(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()
	countItems := func(tx *world.Tx) (n int) {
		for e := range tx.Entities() {
			if e.H().Type() == ItemType {
				n++
			}
		}
		return n
	}
	<-w.Exec(func(tx *world.Tx) {
		wall(tx, 0, 0, 64, 64)
		p := tx.AddEntity(NewPainting(world.EntitySpawnOpts{}, cube.Pos{0, 64, -1}, cube.North, PaintingMotives[0])).(*Painting)
		tx.SetBlock(cube.Pos{0, 64, 0}, nil, nil)
		for tick := int64(1); tick < 100; tick++ {
			p.Tick(tx, tick)
		}
		if countItems(tx) != 0 {
			t.Fatal("painting popped before the 100-tick survival check")
		}
		p.Tick(tx, 100)
		if countItems(tx) != 1 {
			t.Fatalf("unsupported painting dropped %d items, want 1", countItems(tx))
		}

		tx.SetBlock(cube.Pos{0, 64, 0}, block.Stone{}, nil)
		p = tx.AddEntity(NewPainting(world.EntitySpawnOpts{}, cube.Pos{0, 64, -1}, cube.North, PaintingMotives[0])).(*Painting)
		p.Attack(nil, tx)
		if countItems(tx) != 2 {
			t.Fatalf("attacked painting dropped %d items in total, want 2", countItems(tx))
		}
	})
}

func TestPaintingNBTRecoversBlockFromCentredPosition(t *testing.T) {
	for _, facing := range []cube.Direction{cube.North, cube.South, cube.West, cube.East} {
		for _, name := range []string{"Kebab", "Pool", "Wanderer", "Match", "Skeleton", "backyard"} {
			m, _ := PaintingMotiveByName(name)
			pos := cube.Pos{3, 70, -5}
			centre := paintingBox(pos, facing, m)
			saved := PaintingType.EncodeNBT(&world.EntityData{Data: &paintingBehaviour{motive: m, facing: facing, pos: pos}})
			loaded := &world.EntityData{Pos: centre.Min().Add(centre.Max()).Mul(0.5)}
			PaintingType.DecodeNBT(saved, loaded)
			b := loaded.Data.(*paintingBehaviour)
			if b.motive != m || b.facing != facing || b.pos != pos {
				t.Errorf("%s facing %v: loaded %+v, want motive %v pos %v", name, facing, *b, m, pos)
			}
		}
	}
}
