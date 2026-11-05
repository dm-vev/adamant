package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// TestWaterDoesNotFlowIntoSeagrass checks SeagrassBlock.canPlaceLiquid and
// TallSeagrassBlock.canPlaceLiquid, which are false: water that spreads sideways
// leaves the seagrass in the water around it alone, drops nothing and does not
// disturb what stands on the same floor.
func TestWaterDoesNotFlowIntoSeagrass(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		plant []world.Block
	}{
		{"seagrass", []world.Block{SeaGrass{Type: SeaGrassDefault()}}},
		{"tall seagrass", []world.Block{SeaGrass{Type: SeaGrassDoubleBottom()}, SeaGrass{Type: SeaGrassDoubleTop()}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
			t.Cleanup(func() { _ = w.Close() })
			source, plant := cube.Pos{0, 64, 0}, cube.Pos{1, 64, 0}
			w.Do(func(tx *world.Tx) {
				for x := -2; x <= 3; x++ {
					tx.SetBlock(cube.Pos{x, 63, 0}, Stone{}, nil)
				}
				for i, b := range test.plant {
					at := plant.Add(cube.Pos{0, i, 0})
					tx.SetBlock(at, b, nil)
					tx.SetLiquid(at, Water{Depth: 8, Still: true})
				}
				// A flowing source, as an aquifer leaves in the water of an
				// ocean, wakes up and spreads.
				tx.SetLiquid(source, Water{Depth: 8})
			})
			for range 10 {
				w.AdvanceTick()
			}
			w.Do(func(tx *world.Tx) {
				for i, want := range test.plant {
					at := plant.Add(cube.Pos{0, i, 0})
					if got := tx.Block(at); got != want {
						t.Errorf("block at %v = %#v, want %#v", at, got, want)
					}
					if liquid, ok := tx.Liquid(at); !ok || !isSourceWater(liquid) {
						t.Errorf("liquid at %v = %#v, %t; want source water", at, liquid, ok)
					}
				}
				for e := range tx.Entities() {
					t.Errorf("entity %T in the world, want none", e)
				}
			})
		})
	}
}
