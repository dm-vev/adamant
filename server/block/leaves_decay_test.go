package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// TestLeavesDecayAwayFromLogs checks a flagged leaf that no longer reaches a
// log is removed on its random tick, as LeavesBlock.randomTick does, instead
// of staying and dropping its loot again on every random tick.
func TestLeavesDecayAwayFromLogs(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	pos := cube.Pos{0, 64, 0}
	runWorld(w, func(tx *world.Tx) {
		tx.SetBlock(pos, Leaves{Type: OakLeaves(), ShouldUpdate: true}, nil)
		tx.Block(pos).(Leaves).RandomTick(pos, tx, nil)
		if _, ok := tx.Block(pos).(Air); !ok {
			t.Fatalf("decayed leaf is %#v, want air", tx.Block(pos))
		}
	})
}

// TestLeavesFlagDecayWithoutNotifyingNeighbours checks flagging and clearing
// a leaf's update_bit leaves the next leaf alone, so a tree settles instead
// of its leaves flagging one another forever.
func TestLeavesFlagDecayWithoutNotifyingNeighbours(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	log, first, second := cube.Pos{0, 64, 0}, cube.Pos{1, 64, 0}, cube.Pos{2, 64, 0}
	runWorld(w, func(tx *world.Tx) {
		tx.SetBlock(log, Log{Wood: OakWood()}, nil)
		tx.SetBlock(first, Leaves{Type: OakLeaves()}, nil)
		tx.SetBlock(second, Leaves{Type: OakLeaves()}, nil)
	})
	// Let the neighbour updates of building the tree settle, then clear the
	// flags they set.
	redstoneWireTestWaitTick(t, w, 2)
	runWorld(w, func(tx *world.Tx) {
		for _, pos := range []cube.Pos{first, second} {
			tx.Block(pos).(Leaves).RandomTick(pos, tx, nil)
		}
		tx.Block(first).(Leaves).NeighbourUpdateTick(first, log, tx)
	})
	redstoneWireTestWaitTick(t, w, 4)
	runWorld(w, func(tx *world.Tx) {
		if !tx.Block(first).(Leaves).ShouldUpdate {
			t.Fatal("updated leaf was not flagged to check for decay")
		}
		if tx.Block(second).(Leaves).ShouldUpdate {
			t.Fatal("flagging a leaf flagged the leaf next to it")
		}
		tx.Block(first).(Leaves).RandomTick(first, tx, nil)
	})
	redstoneWireTestWaitTick(t, w, 6)
	runWorld(w, func(tx *world.Tx) {
		if tx.Block(first).(Leaves).ShouldUpdate || tx.Block(second).(Leaves).ShouldUpdate {
			t.Fatal("clearing a leaf's flag flagged a leaf again")
		}
	})
}
