package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// ComparatorBlock::use writes the configured mode directly (client
// 0x8e15781). Both positive outputs keep the identifier unchanged, so this
// cannot pass merely because an identifier transition refreshes the mode.
func TestComparatorActivationRefreshesMode(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End}.New()
	t.Cleanup(func() { _ = w.Close() })
	pos := cube.Pos{0, 65, 0}
	<-w.Exec(func(tx *world.Tx) {
		for x := -1; x <= 1; x++ {
			for z := -1; z <= 3; z++ {
				tx.SetBlock(cube.Pos{x, 64, z}, Stone{}, nil)
			}
		}
		tx.SetBlock(cube.Pos{-1, 65, 0}, Cake{}, nil)
		tx.SetBlock(pos, RedstoneComparator{Facing: cube.West}, nil)
		tx.SetBlock(cube.Pos{0, 65, 1}, RedstoneWire{}, nil)
		tx.SetBlock(cube.Pos{0, 65, 2}, RedstoneComparator{Facing: cube.South}, nil)
		tx.SetBlock(cube.Pos{0, 65, 3}, Cake{Bites: 3}, nil)
	})
	for index, expected := range []uint8{14, 6, 14} {
		for range 12 {
			w.AdvanceTick()
		}
		<-w.Exec(func(tx *world.Tx) {
			c := tx.Block(pos).(RedstoneComparator)
			if c.Output != expected || !c.Powered || !c.Lit {
				t.Errorf("output=%d lit=%v powered=%v, expected %d", c.Output, c.Lit, c.Powered, expected)
			}
			if index < 2 {
				c.Activate(pos, cube.FaceUp, tx, nil, nil)
			}
		})
	}
}

type cancelComparatorOutput struct{ world.NopHandler }

func (cancelComparatorOutput) HandleRedstoneUpdate(ctx *world.Context, update world.RedstoneUpdate) {
	if _, ok := update.After.(RedstoneComparator); ok {
		ctx.Cancel()
	}
}

func TestComparatorCancelledPublicationRetries(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End}.New()
	t.Cleanup(func() { _ = w.Close() })
	pos := cube.Pos{0, 65, 0}
	w.Handle(cancelComparatorOutput{})
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos.Side(cube.FaceDown), Stone{}, nil)
		tx.SetBlock(cube.Pos{-1, 64, 0}, Stone{}, nil)
		tx.SetBlock(pos.Side(cube.FaceWest), Cake{}, nil)
		tx.SetBlock(pos, RedstoneComparator{Facing: cube.West}, nil)
	})
	for range 12 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		c := tx.Block(pos).(RedstoneComparator)
		if c.Output != 0 || c.Lit || c.Powered {
			t.Errorf("cancelled output published: %+v", c)
		}
	})
	w.Handle(world.NopHandler{})
	for range 12 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		c := tx.Block(pos).(RedstoneComparator)
		if c.Output != 14 || !c.Lit || !c.Powered {
			t.Errorf("output did not retry: %+v", c)
		}
	})
}
