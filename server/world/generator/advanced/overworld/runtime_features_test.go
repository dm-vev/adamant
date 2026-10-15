package overworld

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
)

// TestRuntimeFeaturesGrowTreesInLiveWorld checks a configured feature placed
// at runtime writes into the live world: an oak grows from dirt, with logs
// above its origin and leaves around them, and does not grow where its
// trunk is blocked.
func TestRuntimeFeaturesGrowTreesInLiveWorld(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		features, ok := tx.Features()
		if !ok {
			t.Fatal("no runtime features registered")
		}
		origin := cube.Pos{0, 64, 0}
		for x := -3; x <= 3; x++ {
			for z := -3; z <= 3; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Dirt{}, nil)
			}
		}
		random := java26.NewLegacyRandom(1)
		if !features.PlaceConfigured(tx, "minecraft:oak", origin, &random) {
			t.Fatal("oak was not placed")
		}
		if _, ok := tx.Block(origin).(block.Log); !ok {
			t.Fatalf("oak origin holds %#v, want a log", tx.Block(origin))
		}
		leaves := 0
		for x := -3; x <= 3; x++ {
			for y := 64; y <= 72; y++ {
				for z := -3; z <= 3; z++ {
					if _, ok := tx.Block(cube.Pos{x, y, z}).(block.Leaves); ok {
						leaves++
					}
				}
			}
		}
		if leaves < 20 {
			t.Fatalf("oak has %d leaves, want a crown", leaves)
		}

		blocked := cube.Pos{20, 64, 0}
		tx.SetBlock(blocked.Side(cube.FaceDown), block.Dirt{}, nil)
		tx.SetBlock(blocked.Add(cube.Pos{0, 2, 0}), block.Stone{}, nil)
		random = java26.NewLegacyRandom(1)
		if features.PlaceConfigured(tx, "minecraft:oak", blocked, &random) {
			t.Fatal("oak grew through stone above its origin")
		}
		if _, ok := tx.Block(blocked).(block.Air); !ok {
			t.Fatalf("failed oak left %#v at its origin", tx.Block(blocked))
		}
	})
}
