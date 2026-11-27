package world_test

import (
	"testing"

	"github.com/dm-vev/adamant/server/world"
	_ "github.com/dm-vev/adamant/server/world/biome"
)

// initBiomes holds the biomes registered as the packages initialise, before
// tests register biomes of their own, such as the probes of
// TestLightningSearchMatchesExecutedClient, which have no Java spawns.
var initBiomes = world.Biomes()

// TestEveryBiomeHasJavaSpawns guards natural spawning, which looks up the
// Java spawn rules of the biome a mob spawns in by its Java name.
func TestEveryBiomeHasSpawns(t *testing.T) {
	t.Parallel()
	for _, b := range initBiomes {
		if _, err := world.BiomeNaturalSpawns(world.CanonicalBiomeName(b)); err != nil {
			t.Errorf("biome %s (Java %s): %v", b.String(), world.CanonicalBiomeName(b), err)
		}
	}
}
