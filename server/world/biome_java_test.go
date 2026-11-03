package world_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	_ "github.com/df-mc/dragonfly/server/world/biome"
)

// initBiomes holds the biomes registered as the packages initialise, before
// tests register biomes of their own, such as the probes of
// TestLightningSearchMatchesExecutedClient, which have no Java spawns.
var initBiomes = world.Biomes()

// TestEveryBiomeHasJavaSpawns guards natural spawning, which looks up the
// Java spawn rules of the biome a mob spawns in by its Java name.
func TestEveryBiomeHasJavaSpawns(t *testing.T) {
	t.Parallel()
	for _, b := range initBiomes {
		if _, err := world.JavaBiomeNaturalSpawns(world.JavaBiomeName(b)); err != nil {
			t.Errorf("biome %s (Java %s): %v", b.String(), world.JavaBiomeName(b), err)
		}
	}
}
