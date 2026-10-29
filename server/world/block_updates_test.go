package world_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// blockUpdateRecorder records the changes of blocks it is shown.
type blockUpdateRecorder struct {
	world.NopViewer
	shown []*world.BlockUpdates
}

func (r *blockUpdateRecorder) ViewBlockUpdates(u *world.BlockUpdates) { r.shown = append(r.shown, u) }

// TestBlockUpdatesShownOncePerTransaction changes blocks of a column in a
// transaction: its viewer is shown them together once the transaction ends,
// the last change of a block changed twice in place of both.
func TestBlockUpdatesShownOncePerTransaction(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	r := &blockUpdateRecorder{}
	<-w.Exec(func(tx *world.Tx) {
		l := world.NewLoader(1, w, r)
		l.Move(tx, mgl64.Vec3{8, 64, 8})
		l.Load(tx, 9)
	})
	w.AdvanceTick()
	r.shown = nil
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{1, 64, 1}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{2, 64, 1}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{1, 64, 1}, block.Dirt{}, nil)
		if len(r.shown) != 0 {
			t.Error("changes shown before the transaction ended")
		}
	})
	if len(r.shown) != 1 {
		t.Fatalf("shown %d sets of changes, want one", len(r.shown))
	}
	got := r.shown[0].Updates
	if len(got) != 2 || got[0].Pos != (cube.Pos{1, 64, 1}) || got[0].Block != (block.Dirt{}) || got[1].Pos != (cube.Pos{2, 64, 1}) {
		t.Fatalf("shown %+v, want the dirt and the stone", got)
	}
}
