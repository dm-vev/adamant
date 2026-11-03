package block_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// TestAbandonedCampChestFillsWithMap fills the chests of abandoned camps,
// whose loot may hold exploration maps to structures of Java 26: each
// fills, and some roll a map, which stays blank in a world without a
// generator to find the structure in.
func TestAbandonedCampChestFillsWithMap(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	maps := 0
	for _, table := range []string{"minecraft:chests/abandoned_camp_common_chest", "minecraft:chests/abandoned_camp_secret_chest"} {
		for seed := range int64(200) {
			pos := cube.Pos{int(seed), 70, 0}
			job := w.Do(func(tx *world.Tx) {
				chest := world.DecodeNBT(block.NewChest(), map[string]any{"id": "Chest", "LootTable": table, "LootTableSeed": seed}, world.DefaultBlockRegistry).(block.Chest)
				tx.SetBlock(pos, chest, nil)
				if err := tx.Block(pos).(block.Chest).GenerateLoot(tx, pos); err != nil {
					t.Errorf("%s with seed %d: %v", table, seed, err)
					return
				}
				for _, s := range tx.Block(pos).(block.Chest).Inventory(tx, pos).Items() {
					switch s.Item().(type) {
					case item.EmptyMap, item.FilledMap:
						maps++
					}
				}
			})
			<-job.Done()
			if err := job.Err(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if maps == 0 {
		t.Fatal("no chest of an abandoned camp rolled an exploration map in 400 seeds")
	}
}
