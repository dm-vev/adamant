package block_test

import (
	"math/rand/v2"
	"testing"

	. "github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
)

// TestKelpChangeAtTheTopLeavesTheFloor grows and cuts a kelp column that
// stands on gravel over water, as an aquifer leaves under an ocean floor.
// Java changes only the cell under the change: GrowingPlantHeadBlock turns
// the old head into a body, GrowingPlantBodyBlock turns the cell under a
// broken head into a head, and the body under either keeps its state. The
// floor of a column taller than that hears nothing and does not fall, and
// the column keeps standing.
func TestKelpChangeAtTheTopLeavesTheFloor(t *testing.T) {
	t.Parallel()
	const height = 4
	floor, foot := cube.Pos{0, 63, 0}, cube.Pos{0, 64, 0}
	head := foot.Add(cube.Pos{0, height - 1, 0})
	for _, test := range []struct {
		name   string
		change func(t *testing.T, tx *world.Tx)
		// kelp is the number of kelp cells the column holds after the change.
		kelp int
	}{
		{"growth", func(t *testing.T, tx *world.Tx) {
			r := rand.New(rand.NewPCG(1, 2))
			for range 1000 {
				tx.Block(head).(Kelp).RandomTick(head, tx, r)
				if _, ok := tx.Block(head.Side(cube.FaceUp)).(Kelp); ok {
					return
				}
			}
			t.Fatal("kelp did not grow in 1000 random ticks")
		}, height + 1},
		{"head broken", func(t *testing.T, tx *world.Tx) {
			tx.SetBlock(head, Water{Depth: 8, Still: true}, nil)
		}, height - 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry, RandomTickSpeed: -1}.New()
			t.Cleanup(func() { _ = w.Close() })
			var body []world.Block
			quiet := &world.SetOpts{DisableBlockUpdates: true}
			w.Do(func(tx *world.Tx) {
				// A basin of stone holds water under a gravel floor and
				// above it, built as generation leaves it: without updates.
				for x := -3; x <= 3; x++ {
					for z := -3; z <= 3; z++ {
						for y := 60; y <= 70; y++ {
							pos := cube.Pos{x, y, z}
							switch {
							case y == 60 || x == -3 || x == 3 || z == -3 || z == 3:
								tx.SetBlock(pos, Stone{}, quiet)
							case pos[1] == floor[1]:
								tx.SetBlock(pos, Gravel{}, quiet)
							default:
								tx.SetBlock(pos, Water{Depth: 8, Still: true}, quiet)
							}
						}
					}
				}
				for i := range height {
					at := foot.Add(cube.Pos{0, i, 0})
					tx.SetBlock(at, Kelp{Age: 3 + i}, quiet)
				}
				for i := range height - 1 {
					body = append(body, tx.Block(foot.Add(cube.Pos{0, i, 0})))
				}
			})
			w.Do(func(tx *world.Tx) { test.change(t, tx) })
			for range 20 {
				w.AdvanceTick()
			}
			w.Do(func(tx *world.Tx) {
				if _, ok := tx.Block(floor).(Gravel); !ok {
					t.Errorf("floor at %v = %#v, want the gravel that was there", floor, tx.Block(floor))
				}
				for i := range test.kelp {
					at := foot.Add(cube.Pos{0, i, 0})
					if _, ok := tx.Block(at).(Kelp); !ok {
						t.Errorf("block at %v = %#v, want kelp", at, tx.Block(at))
					}
				}
				// The body under the change keeps its state.
				for i, want := range body[:test.kelp-2] {
					at := foot.Add(cube.Pos{0, i, 0})
					if got := tx.Block(at); got != want {
						t.Errorf("body at %v = %#v, want %#v as before", at, got, want)
					}
				}
				for e := range tx.Entities() {
					t.Errorf("entity %s in the world, want none", e.H().Type().EncodeEntity())
				}
			})
		})
	}
}
