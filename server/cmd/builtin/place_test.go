package builtin

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
)

// TestPlaceTemplateRotatesAroundItsPosition checks /place template: unlike a
// structure block, it turns the template around the position given, as
// Java's PlaceCommand does, and reports unknown templates.
func TestPlaceTemplateRotatesAroundItsPosition(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	place := cmd.New("place", "", nil, placeTemplateCommand{})
	<-w.Exec(func(tx *world.Tx) {
		src := cube.Pos{0, 70, 0}
		tx.SetBlock(src, block.Stone{}, nil)
		tx.SetBlock(src.Add(cube.Pos{2, 0, 0}), block.Gold{}, nil)
		tx.SetBlock(src.Add(cube.Pos{0, 0, 1}), block.Lapis{}, nil)
		if err := w.SaveStructureTemplate("row", tx.CaptureStructure(src, [3]int{3, 1, 2}, false), false); err != nil {
			t.Fatal(err)
		}
		if out := runTestCommand(place, "template row 10 70 10 clockwise_90", tx); out.ErrorCount() != 0 {
			t.Fatalf("placing the row failed: %v", out.Errors())
		}
		// Clockwise around (10, 70, 10), +X turns into +Z and +Z into -X.
		for pos, want := range map[cube.Pos]world.Block{{10, 70, 10}: block.Stone{}, {10, 70, 12}: block.Gold{}, {9, 70, 10}: block.Lapis{}} {
			if got := tx.Block(pos); got != want {
				t.Errorf("block at %v is %T, want %T", pos, got, want)
			}
		}
		if out := runTestCommand(place, "template missing 10 70 10", tx); out.ErrorCount() == 0 {
			t.Fatal("placing a template that does not exist succeeded")
		}
	})
}
