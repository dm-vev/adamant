package world_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	_ "github.com/df-mc/dragonfly/server/world/biome"
)

// TestEveryBiomeHasJavaSpawns guards natural spawning, which looks up the
// Java spawn rules of the biome a mob spawns in by its Java name.
func TestEveryBiomeHasJavaSpawns(t *testing.T) {
	for _, b := range world.Biomes() {
		if _, err := world.JavaBiomeNaturalSpawns(world.JavaBiomeName(b)); err != nil {
			t.Errorf("biome %s (Java %s): %v", b.String(), world.JavaBiomeName(b), err)
		}
	}
}
