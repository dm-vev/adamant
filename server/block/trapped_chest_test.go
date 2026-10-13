package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

type chestTestViewer struct {
	world.NopViewer
	id int
}

func (chestTestViewer) ViewSlotChange(int, item.Stack) {}

func TestTrappedChestPowerFollowsViewers(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		pos, below := cube.Pos{0, 64, 0}, cube.Pos{0, 63, 0}
		tx.SetBlock(below, Stone{}, nil)
		c := NewTrappedChest()
		tx.SetBlock(pos, c, nil)
		a, b := &chestTestViewer{id: 1}, &chestTestViewer{id: 2}
		power := func() (int, int) {
			for range 2 {
				w.AdvanceTick()
			}
			ch := tx.Block(pos).(TrappedChest)
			return ch.RedstonePower(pos, tx, cube.FaceNorth), tx.RedstoneStrongPower(below)
		}
		if p, strong := power(); p != 0 || strong != 0 {
			t.Fatalf("closed chest power=%d strong below=%d", p, strong)
		}
		tx.Block(pos).(TrappedChest).AddViewer(a, tx, pos)
		tx.Block(pos).(TrappedChest).AddViewer(b, tx, pos)
		if p, strong := power(); p != 2 || strong != 2 {
			t.Fatalf("two viewers: power=%d strong below=%d, want 2/2", p, strong)
		}
		tx.Block(pos).(TrappedChest).RemoveViewer(a, tx, pos)
		if p, _ := power(); p != 1 {
			t.Fatalf("one viewer: power=%d, want 1", p)
		}
		if got := tx.Block(pos).(TrappedChest).RedstoneStrongPower(pos, tx, cube.FaceNorth); got != 0 {
			t.Fatalf("strong power sideways = %d, want 0", got)
		}
	})
}
