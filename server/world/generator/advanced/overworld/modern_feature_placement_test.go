package overworld

import (
	"reflect"
	"testing"

	java26 "github.com/df-mc/dragonfly/server/internal/java26"
)

func TestPlacementOrderMatchesJava263(t *testing.T) {
	// Captured from executed Java FeaturePlacer, including a feature consuming
	// nextInt(7). Later positions must run even after an earlier placement succeeds.
	uniform := func(min, max float64) map[string]any {
		return map[string]any{"type": "minecraft:uniform", "min_inclusive": min, "max_inclusive": max}
	}
	modifiers := []map[string]any{
		{"type": "count", "count": float64(3)},
		{"type": "in_square"},
		{"type": "count", "count": uniform(1, 3)},
		{"type": "offset", "x": uniform(-2, 2), "y": uniform(-1, 1), "z": uniform(-2, 2)},
		{"type": "rarity_filter", "chance": float64(2)},
	}
	for _, tc := range []struct {
		name   string
		random java26.Random
		want   [][4]int32
		placed bool
		next   int64
	}{
		{"legacy123", java26.NewLegacyRandom(123), [][4]int32{{10, 65, 1, 2}, {11, 65, 4, 3}, {15, 64, 9, 6}}, false, -7837950195727116076},
		{"legacy9", java26.NewLegacyRandom(9), [][4]int32{{10, 64, 5, 0}, {11, 64, 7, 0}, {8, 63, 12, 1}}, true, 2166546458748118007},
		{"xoroshiro123", java26.NewXoroshiroRandom(123), [][4]int32{{8, 63, 10, 3}, {17, 64, 7, 3}, {14, 65, 7, 0}, {10, 65, 10, 2}}, true, -7253815056382962531},
		{"xoroshiro9", java26.NewXoroshiroRandom(9), [][4]int32{{7, 64, 8, 0}, {9, 65, 10, 4}, {13, 63, 17, 5}, {8, 65, 16, 5}}, true, -5075633833688250805},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got [][4]int32
			f := &ModernFeatures{}
			placed := f.placeModifiedPositions(nil, modifiers, featurePos{0, 64, 0}, &tc.random, "", func(p featurePos) bool {
				draw := tc.random.NextInt(7)
				got = append(got, [4]int32{p.x, p.y, p.z, draw})
				return draw == 0
			})
			if !reflect.DeepEqual(got, tc.want) || placed != tc.placed {
				t.Fatalf("positions/draws=%v placed=%v; Java wants %v placed=%v", got, placed, tc.want, tc.placed)
			}
			if next := tc.random.NextLong(); next != tc.next {
				t.Fatalf("following stream=%d; Java wants %d", next, tc.next)
			}
		})
	}
}

func TestCuboidPlacementMatchesJava263(t *testing.T) {
	f := &ModernFeatures{}
	random := java26.NewLegacyRandom(1)
	got := f.modifyPlacement(nil, featurePos{-1, 10, 4}, map[string]any{
		"type": "cuboid", "xz_size": float64(2), "y_size": float64(2), "include_edges": false, "include_interior": false,
	}, &random, "", nil)
	// Executed Java emits face centres in X/Y/Z order, excluding edges and centre.
	want := []featurePos{{-1, 11, 5}, {0, 10, 5}, {0, 11, 4}, {0, 11, 6}, {0, 12, 5}, {1, 11, 5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cuboid=%v; Java wants %v", got, want)
	}
}

func TestRuleProviderEmptyMatchContinues(t *testing.T) {
	// Executed Java skips the first matching rule when its nested provider has
	// no optional state, rather than taking the current world state or fallback.
	empty := map[string]any{"type": "rule_based", "rules": []any{}}
	predicate := map[string]any{"type": "true"}
	provider := map[string]any{
		"type": "rule_based", "fallback": map[string]any{"id": "minecraft:gold_block"},
		"rules": []any{
			map[string]any{"if_true": predicate, "then": empty},
			map[string]any{"if_true": predicate, "then": map[string]any{"id": "minecraft:diamond_block"}},
		},
	}
	random := java26.NewLegacyRandom(123)
	state, ok := sampleJavaBlockState(nil, featurePos{}, provider, &random)
	if !ok || state.State.Name != "minecraft:diamond_block" {
		t.Fatalf("optional rule selected %+v, present=%v; Java selects diamond_block", state, ok)
	}
	if next := random.NextLong(); next != -5106534569952410475 {
		t.Fatalf("following random stream=%d; Java wants -5106534569952410475", next)
	}
}
