package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestSuspiciousBlocksAdvanceBrushProgress(t *testing.T) {
	for _, test := range []struct {
		name string
		block world.Block
	}{
		{name: "sand", block: SuspiciousSand{}},
		{name: "gravel", block: SuspiciousGravel{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := world.Config{Synchronous: true}.New()
			defer w.Close()
			pos := cube.Pos{0, 10, 0}
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(pos.Side(cube.FaceDown), Stone{}, nil)
				tx.SetBlock(pos, test.block, nil)
			})

			for stroke := range 3 {
				completed := false
				var brushErr error
				<-w.Exec(func(tx *world.Tx) {
					brushable := tx.Block(pos).(interface {
						BrushAt(cube.Pos, cube.Face, *world.Tx, item.User, item.Stack) (bool, error)
					})
					completed, brushErr = brushable.BrushAt(pos, cube.FaceNorth, tx, nil, item.Stack{})
				})
				if brushErr != nil {
					t.Fatalf("brush stroke %d: %v", stroke+1, brushErr)
				}
				if completed {
					t.Fatalf("stroke %d unexpectedly completed the block", stroke+1)
				}
				if stroke != 2 {
					for range 10 {
						w.AdvanceTick()
					}
				}
			}

			var got world.Block
			<-w.Exec(func(tx *world.Tx) {
				got = tx.Block(pos)
			})
			switch block := got.(type) {
			case SuspiciousSand:
				if block.Dusted != 2 || block.brushCount != 3 || !block.HasHitDirection || block.HitDirection != cube.FaceNorth {
					t.Fatalf("unexpected suspicious sand state: %+v", block)
				}
			case SuspiciousGravel:
				if block.Dusted != 2 || block.brushCount != 3 || !block.HasHitDirection || block.HitDirection != cube.FaceNorth {
					t.Fatalf("unexpected suspicious gravel state: %+v", block)
				}
			default:
				t.Fatalf("unexpected block after brushing: %T", got)
			}
		})
	}
}

// TestSuspiciousBlocksResetThroughScheduledTicks checks suspicious blocks
// do not tick every tick, as BrushableBlockEntity has no ticker, and that
// brushing left off still resets through the scheduled ticks of
// BrushableBlock, as checkReset does.
func TestSuspiciousBlocksResetThroughScheduledTicks(t *testing.T) {
	for _, b := range []world.Block{SuspiciousSand{}, SuspiciousGravel{}} {
		if _, ok := b.(world.TickerBlock); ok {
			t.Errorf("%T ticks every tick", b)
		}
	}
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	pos := cube.Pos{0, 10, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos.Side(cube.FaceDown), Stone{}, nil)
		tx.SetBlock(pos, SuspiciousGravel{}, nil)
		for range 3 {
			brushable := tx.Block(pos).(interface {
				BrushAt(cube.Pos, cube.Face, *world.Tx, item.User, item.Stack) (bool, error)
			})
			if _, err := brushable.BrushAt(pos, cube.FaceNorth, tx, nil, item.Stack{}); err != nil {
				t.Fatal(err)
			}
		}
	})
	for range 80 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		got, ok := tx.Block(pos).(SuspiciousGravel)
		if !ok {
			t.Fatalf("block after brushing left off is %T", tx.Block(pos))
		}
		if got.brushCount != 0 || got.Dusted != 0 || got.HasHitDirection {
			t.Fatalf("brushing left off for 80 ticks not reset: %+v", got)
		}
	})
}
