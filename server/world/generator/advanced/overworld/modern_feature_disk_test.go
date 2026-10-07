package overworld

import (
	"fmt"
	"testing"

	java26 "github.com/df-mc/dragonfly/server/internal/java26"
)

// Positions and material sequences were captured from Java 26.3 DiskFeature.
// The centre column has air at y=9, so a rejected target must not sample a state.
var diskJavaPositions = [...]featurePos{
	{8, 11, 7}, {8, 10, 7}, {8, 9, 7},
	{7, 11, 8}, {7, 10, 8}, {7, 9, 8},
	{8, 11, 8}, {8, 10, 8},
	{9, 11, 8}, {9, 10, 8}, {9, 9, 8},
	{8, 11, 9}, {8, 10, 9}, {8, 9, 9},
}

func diskTestRegion(t *testing.T) *featureRegion {
	t.Helper()
	r := countLayerTestRegion()
	stone := countLayerNativeState(t, "stone")
	for _, p := range diskJavaPositions {
		if !r.Set(p, stone) {
			t.Fatalf("failed to initialise disk target %v", p)
		}
	}
	return r
}

func diskTestWeightedProvider() map[string]any {
	return map[string]any{"type": "weighted", "entries": []any{
		map[string]any{"data": "minecraft:gold_block", "weight": float64(1)},
		map[string]any{"data": "minecraft:diamond_block", "weight": float64(2)},
		map[string]any{"data": "minecraft:clay", "weight": float64(3)},
	}}
}

func checkDiskMaterials(t *testing.T, r *featureRegion, want string) {
	t.Helper()
	materials := map[byte]string{'G': "minecraft:gold_block", 'D': "minecraft:diamond_block", 'C': "minecraft:clay", 'S': "minecraft:stone"}
	for i, p := range diskJavaPositions {
		if name, _ := r.State(p); name != materials[want[i]] {
			t.Errorf("disk position %v: got %s, Java wants %s", p, name, materials[want[i]])
		}
	}
	for _, p := range []featurePos{{8, 9, 8}, {7, 10, 7}, {9, 10, 7}, {7, 10, 9}, {9, 10, 9}, {8, 8, 8}, {8, 12, 8}} {
		if name, _ := r.State(p); name != "minecraft:air" {
			t.Errorf("disk replaced non-target %v with %s", p, name)
		}
	}
}

func TestDiskWeightedPlacementMatchesJava263(t *testing.T) {
	for _, tc := range []struct {
		xoroshiro bool
		seed      int64
		materials string
		next      int64
	}{
		{false, 123, "DDDCCCGDCDCGDC", 1326634973105178603},
		{false, 9, "DCCDCCCGDDDCCC", -7746595021045585084},
		{true, 123, "DCCCGGCCGCCDCC", 8325783311766438723},
		{true, 9, "DCCCCDCGCCCDCC", -4696002023940045647},
	} {
		t.Run(fmt.Sprintf("xoro=%v/seed=%d", tc.xoroshiro, tc.seed), func(t *testing.T) {
			r := diskTestRegion(t)
			random := java26.NewLegacyRandom(tc.seed)
			if tc.xoroshiro {
				random = java26.NewXoroshiroRandom(tc.seed)
			}
			config := map[string]any{
				"radius": float64(1), "half_height": float64(1), "state_provider": diskTestWeightedProvider(),
				"target": map[string]any{"type": "matching_blocks", "blocks": "minecraft:stone"},
			}
			if !placeModernDiskRegion(r, config, featurePos{8, 10, 8}, &random) {
				t.Fatal("disk reported no placements")
			}
			checkDiskMaterials(t, r, tc.materials)
			if next := random.NextLong(); next != tc.next {
				t.Errorf("following stream=%d, Java wants %d", next, tc.next)
			}
		})
	}
}

func TestDiskContextualOptionalStates(t *testing.T) {
	for _, tc := range []struct {
		name      string
		provider  any
		target    string
		placed    bool
		materials string
	}{
		{"contextual", map[string]any{"type": "rule_based", "rules": []any{map[string]any{
			"if_true": map[string]any{"type": "matching_blocks", "blocks": "minecraft:stone", "offset": []any{float64(0), float64(-1), float64(0)}},
			"then":    map[string]any{"id": "minecraft:gold_block"},
		}}}, "minecraft:stone", true, "GGSGGSGSGGSGGS"},
		{"empty", map[string]any{"type": "rule_based", "rules": []any{}}, "minecraft:stone", false, "SSSSSSSSSSSSSS"},
		{"no_target", diskTestWeightedProvider(), "minecraft:bedrock", false, "SSSSSSSSSSSSSS"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := diskTestRegion(t)
			random := java26.NewLegacyRandom(123)
			config := map[string]any{
				"radius": float64(1), "half_height": float64(1), "state_provider": tc.provider,
				"target": map[string]any{"type": "matching_blocks", "blocks": tc.target},
			}
			if placed := placeModernDiskRegion(r, config, featurePos{8, 10, 8}, &random); placed != tc.placed {
				t.Errorf("placed=%v, Java wants %v", placed, tc.placed)
			}
			checkDiskMaterials(t, r, tc.materials)
			if next := random.NextLong(); next != -5106534569952410475 {
				t.Errorf("provider sampled without an eligible state: next=%d", next)
			}
		})
	}
}
