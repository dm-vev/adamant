package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestUnsupportedHookRespectsDisabledBlockDrops(t *testing.T) {
	// With drops disabled, breaking a supported block must not invoke an item
	// entity factory. This world deliberately has no such factory registered.
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	if err := w.SetGameRule("block_drops", false); err != nil {
		t.Fatal(err)
	}
	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{1, 65, 0}
		support := pos.Side(cube.FaceWest)
		hook := TripwireHook{Facing: cube.East}
		tx.SetBlock(support, Stone{}, nil)
		tx.SetBlock(pos, hook, nil)
		tx.SetBlock(support, nil, nil)
		hook.NeighbourUpdateTick(pos, support, tx)
		if _, ok := tx.Block(pos).(Air); !ok {
			t.Fatalf("unsupported hook was not removed: %T", tx.Block(pos))
		}
	})
}
