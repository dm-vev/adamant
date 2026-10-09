package nether

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestLocateUsesCandidateRatherThanRegionInput(t *testing.T) {
	g := NewNether(0)
	// Both positions belong to the same nether_complexes placement region.
	// Compare the real planner's choice, not a pinned legacy fortress/bastion RNG.
	regionOrigin, candidateOrigin := cube.Pos{0, 64, 27 * 16}, cube.Pos{0, 64, 44 * 16}
	for _, selector := range []string{"minecraft:fortress", "minecraft:bastion_remnant"} {
		t.Run(selector, func(t *testing.T) {
			expected, valid, err := g.FindNearestMapFeature(selector, candidateOrigin, 0, nil)
			if err != nil {
				t.Fatal(err)
			}
			got, found, err := g.FindNearestMapFeature(selector, regionOrigin, 0, nil)
			if err != nil || found != valid || got != expected {
				t.Fatalf("region locate=(%+v,%v,%v), candidate lookup=(%+v,%v)", got, found, err, expected, valid)
			}
			if valid {
				_, found, err = g.FindNearestMapFeature(selector, regionOrigin, 0, func(location world.StructureLocation) bool { return location.StartChunk == expected.StartChunk })
				if err != nil || found {
					t.Fatalf("referenced start was not excluded: found=%v err=%v", found, err)
				}
			}
		})
	}
}
