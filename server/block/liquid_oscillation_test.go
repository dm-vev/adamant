package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// blockUpdateCounter counts the blocks sent to it.
type blockUpdateCounter struct {
	world.NopViewer
	updates int
}

func (c *blockUpdateCounter) ViewBlockUpdates(u *world.BlockUpdates) { c.updates += len(u.Updates) }

// TestFallingWaterKeepsTheWaterBesideItsFoot lets water fall off a ledge onto
// a floor and spread. FlowingFluid.getNewLiquid takes the amount of a falling fluid, 8,
// for the highest neighbour of the cells beside it, which keep the 7 that
// FlowingFluid.spreadToSides gave them. Taking a falling neighbour for a
// water of the depth it is stored at, they found no source and drained to 5
// until the foot of the stream, which still did not know them, spread to them
// again: the same blocks were sent to every viewer every fifth tick.
func TestFallingWaterKeepsTheWaterBesideItsFoot(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	viewer := &blockUpdateCounter{}
	w.Do(func(tx *world.Tx) {
		for x := -9; x <= 9; x++ {
			for z := -9; z <= 9; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, Stone{}, nil)
			}
		}
		world.NewLoader(2, w, viewer).Move(tx, mgl64.Vec3{0, 64, 0})
		// A source on a ledge spreads to its edge, and falls off it as
		// water of depth 7: the stream keeps the depth of what feeds it.
		for x := -3; x <= -1; x++ {
			tx.SetBlock(cube.Pos{x, 65, 0}, Stone{}, nil)
		}
		tx.SetLiquid(cube.Pos{-1, 66, 0}, Water{Depth: 8, Still: true})
	})
	w.Do(func(tx *world.Tx) { world.NewLoader(2, w, viewer).Load(tx, 100) })
	// The pool settles within the spread of the water.
	for range 200 {
		w.AdvanceTick()
	}
	before := viewer.updates
	for range 100 {
		w.AdvanceTick()
	}
	if n := viewer.updates - before; n != 0 {
		t.Errorf("%d block updates in 100 ticks of a settled pool of water, want none", n)
	}
	w.Do(func(tx *world.Tx) {
		beside, ok := tx.Liquid(cube.Pos{1, 64, 0})
		if !ok || beside.LiquidDepth() != 7 || beside.LiquidFalling() {
			t.Errorf("the water beside the foot of the stream = %#v, %t; want a depth of 7", beside, ok)
		}
	})
}
