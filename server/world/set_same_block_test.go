package world_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// blockUpdateCounter counts the blocks it is sent.
type blockUpdateCounter struct {
	world.NopViewer
	updates int
}

func (c *blockUpdateCounter) ViewBlockUpdates(u *world.BlockUpdates) { c.updates += len(u.Updates) }

// TestSettingTheStateABlockHasChangesNothing checks Level.setBlock of the
// state a block has: LevelChunk.setBlockState returns null and no viewer is
// sent the block, and the same for a liquid that flows into where it is.
func TestSettingTheStateABlockHasChangesNothing(t *testing.T) {
	w := world.Config{Generator: world.NopGenerator{}, Provider: world.NopProvider{}, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	// The water would flow and drain on its own.
	w.Freeze()
	viewer := &blockUpdateCounter{}
	pos := cube.Pos{3, 10, 3}
	<-w.Exec(func(tx *world.Tx) {
		loader := world.NewLoader(2, w, viewer)
		loader.Move(tx, mgl64.Vec3{3, 10, 3})
		loader.Load(tx, 1)
	})
	set := func(what string, want int, f func(tx *world.Tx)) {
		t.Helper()
		before := viewer.updates
		<-w.Exec(f)
		if got := viewer.updates - before; got != want {
			t.Errorf("%s sent %d block updates, want %d", what, got, want)
		}
	}
	set("setting stone", 1, func(tx *world.Tx) { tx.SetBlock(pos, block.Stone{}, nil) })
	set("setting stone again", 0, func(tx *world.Tx) { tx.SetBlock(pos, block.Stone{}, nil) })
	set("setting dirt", 1, func(tx *world.Tx) { tx.SetBlock(pos, block.Dirt{}, nil) })
	set("setting air over air", 0, func(tx *world.Tx) { tx.SetBlock(pos.Side(cube.FaceUp), nil, nil) })
	water := block.Water{Depth: 3, Still: true}
	set("flowing water", 1, func(tx *world.Tx) {
		tx.SetBlock(pos.Add(cube.Pos{2, 0, 0}), nil, nil)
		tx.SetLiquid(pos.Add(cube.Pos{2, 0, 0}), water)
	})
	set("the same water again", 0, func(tx *world.Tx) { tx.SetLiquid(pos.Add(cube.Pos{2, 0, 0}), water) })
	set("water one deeper", 1, func(tx *world.Tx) { tx.SetLiquid(pos.Add(cube.Pos{2, 0, 0}), block.Water{Depth: 4, Still: true}) })
}
