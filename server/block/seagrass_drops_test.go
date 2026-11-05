package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// TestSeagrassDropsOnlyWithShears checks the loot tables of seagrass and tall
// seagrass: tool/can_shear leaves a plant that breaks without shears, as by
// hand or when its floor goes, without a drop.
func TestSeagrassDropsOnlyWithShears(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		plant     SeaGrass
		withHand  int
		withShear int
	}{
		{"seagrass", SeaGrass{Type: SeaGrassDefault()}, 0, 1},
		{"lower half", SeaGrass{Type: SeaGrassDoubleBottom()}, 0, 2},
		{"upper half", SeaGrass{Type: SeaGrassDoubleTop()}, 0, 2},
	} {
		count := func(tool item.Tool) (n int) {
			for _, drop := range test.plant.BreakInfo().Drops(tool, nil) {
				if _, ok := drop.Item().(SeaGrass); !ok {
					t.Errorf("%s drops %T, want seagrass", test.name, drop.Item())
				}
				n += drop.Count()
			}
			return n
		}
		if n := count(item.ToolNone{}); n != test.withHand {
			t.Errorf("%s by hand drops %d, want %d", test.name, n, test.withHand)
		}
		if n := count(item.Shears{}); n != test.withShear {
			t.Errorf("%s with shears drops %d, want %d", test.name, n, test.withShear)
		}
	}
}

// TestSeagrassBreaksWithoutItem checks that a seagrass that breaks away, as
// when its water or its floor is gone, spawns no item. This world has no item
// entity factory, so that a drop would panic.
func TestSeagrassBreaksWithoutItem(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	pos := cube.Pos{0, 64, 0}
	w.Do(func(tx *world.Tx) {
		plant := SeaGrass{Type: SeaGrassDefault()}
		tx.SetBlock(pos, plant, nil)
		breakBlock(plant, pos, tx)
		if b := tx.Block(pos); b != (Air{}) {
			t.Errorf("broken seagrass left %#v, want air", b)
		}
	})
}
