package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestLeavesLootTables(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	seen := map[string]bool{}
	job := w.Do(func(tx *world.Tx) {
		for range 1000 {
			for _, drop := range leavesDrops(Leaves{Type: OakLeaves()}, item.ToolNone{}, nil, tx, cube.Pos{}) {
				name, _ := drop.Item().EncodeItem()
				seen[name] = true
			}
		}
	})
	<-job.Done()
	if err := job.Err(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"minecraft:oak_sapling", "minecraft:stick", "minecraft:apple"} {
		if !seen[name] {
			t.Errorf("decay loot never produced %s", name)
		}
	}
	leaves := Leaves{Type: OakLeaves()}
	drops := leaves.BreakInfo().Drops(item.Shears{}, nil)
	if len(drops) != 1 {
		t.Fatalf("shears drops = %v, want leaves block", drops)
	}
	name, _ := drops[0].Item().EncodeItem()
	if name != "minecraft:oak_leaves" {
		t.Fatalf("shears drop = %s, want minecraft:oak_leaves", name)
	}
}
