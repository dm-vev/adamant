package overworld

import (
	"testing"

	"github.com/df-mc/dragonfly/server/internal/vanilla"
)

func TestRandomNeighborSpreadCountsHolderSetMembers(t *testing.T) {
	// The builtin weeping-vines feature accepts a direct holder list, unlike
	// glowstone's single holder. Placement requires exactly one matching face.
	for _, tc := range []struct{ name, neighbor, want string }{
		{"one accepted neighbor", "air", "nether_wart_block"},
		{"two different accepted neighbors", "netherrack", "air"},
		{"unaccepted neighbor ignored", "stone", "nether_wart_block"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			region := countLayerTestRegion()
			origin, target := featurePos{8, 8, 8}, featurePos{8, 9, 8}
			region.Set(featurePos{9, 9, 8}, countLayerNativeState(t, tc.neighbor))
			config := map[string]any{
				"block":              map[string]any{"id": "minecraft:nether_wart_block"},
				"accepted_neighbors": []any{"minecraft:netherrack", "minecraft:nether_wart_block"},
				"attempts":           float64(1),
				"xz_offset":          map[string]any{"type": "minecraft:uniform", "min_inclusive": float64(0), "max_inclusive": float64(0)},
				"y_offset":           map[string]any{"type": "minecraft:uniform", "min_inclusive": float64(1), "max_inclusive": float64(1)},
				"can_replace":        map[string]any{"type": "minecraft:matching_blocks", "blocks": "minecraft:air"},
			}
			random := vanilla.NewLegacyRandom(42)
			placeModernRandomNeighborSpread(region, config, origin, &random)
			if actual, _ := region.State(target); normalizeResource(actual) != tc.want {
				t.Fatalf("target=%s want=%s", actual, tc.want)
			}
			if actual, _ := region.State(origin); normalizeResource(actual) != "nether_wart_block" {
				t.Fatalf("origin=%s want=nether_wart_block", actual)
			}
		})
	}
}
