package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/overworld"
)

// TestChunksSpawnOriginalCreatures checks
// NaturalSpawner.spawnMobsForChunkGeneration: newly generated Overworld land
// comes with the animals of its biomes.
func TestChunksSpawnOriginalCreatures(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	w := world.Config{Entities: entity.DefaultRegistry, Generator: overworld.NewOverworld(42), Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		for cx := -6; cx <= 6; cx++ {
			for cz := -6; cz <= 6; cz++ {
				tx.Block(cube.Pos{cx * 16, 0, cz * 16})
			}
		}
		creatures := map[string]int{}
		for e := range tx.Entities() {
			if _, ok := e.(entity.Living); ok {
				creatures[e.H().Type().EncodeEntity()]++
				if e.H().SpawnReason() != "chunk_generation" {
					t.Errorf("%s spawned for %q, want chunk_generation", e.H().Type().EncodeEntity(), e.H().SpawnReason())
				}
			}
		}
		total := 0
		for _, n := range creatures {
			total += n
		}
		t.Log(creatures)
		if total == 0 {
			t.Fatal("169 newly generated Overworld chunks spawned no creatures")
		}
	})
}
