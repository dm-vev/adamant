package world_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestPositionTrackingReplacementPaths(t *testing.T) {
	for _, test := range []struct {
		name    string
		replace func(*world.Tx, cube.Pos, world.Block)
	}{
		{"SetBlock", func(tx *world.Tx, pos cube.Pos, replacement world.Block) { tx.SetBlock(pos, replacement, nil) }},
		{"SetBlockEntity", func(tx *world.Tx, pos cube.Pos, _ world.Block) { tx.SetBlockEntity(pos, block.Lodestone{}) }},
		{"BuildStructure", func(tx *world.Tx, pos cube.Pos, replacement world.Block) {
			tx.BuildStructure(pos, singleBlockStructure{replacement})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := world.Config{Synchronous: true}.New()
			defer w.Close()
			pos := cube.Pos{4, 64, 4}
			oldHandle := w.TrackPosition(pos, 0)
			w.Do(func(tx *world.Tx) {
				tx.SetBlock(pos, block.Lodestone{}.WithTrackingHandle(oldHandle), nil)
				test.replace(tx, pos, block.Air{})
			})
			if _, _, ok := w.TrackedPosition(oldHandle); ok {
				t.Fatalf("handle %d remained active", oldHandle)
			}
		})
	}
}

func TestPositionTrackingSamePositionReplacementUsesNewHandle(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	pos, temporaryPos := cube.Pos{4, 64, 4}, cube.Pos{5, 64, 4}
	stale := w.TrackPosition(pos, 0)
	fresh := w.TrackPosition(temporaryPos, 0)
	w.Do(func(tx *world.Tx) {
		tx.SetBlock(pos, block.Lodestone{}.WithTrackingHandle(stale), nil)
		tx.SetBlock(pos, block.Lodestone{}.WithTrackingHandle(fresh), nil)
	})
	if _, _, ok := w.TrackedPosition(stale); ok {
		t.Fatalf("stale handle %d was revived", stale)
	}
	if got, _, ok := w.TrackedPosition(fresh); !ok || got != pos {
		t.Fatalf("fresh target = %v, %v; want %v, true", got, ok, pos)
	}
}

type singleBlockStructure struct{ block world.Block }

func (singleBlockStructure) Dimensions() [3]int { return [3]int{1, 1, 1} }
func (s singleBlockStructure) At(int, int, int, func(int, int, int) world.Block) (world.Block, world.Liquid) {
	return s.block, nil
}
