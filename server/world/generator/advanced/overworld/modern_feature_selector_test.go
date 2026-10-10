package overworld

import (
	"fmt"
	"testing"

	java26 "github.com/df-mc/dragonfly/server/internal/java26"
)

func selectorTestRegion() *featureRegion {
	r := countLayerTestRegion()
	r.f.configured = map[string]modernFeatureConfig{"marker": {typeName: "simple_block", config: map[string]any{"to_place": map[string]any{"id": "minecraft:stone"}}}}
	r.f.placed = make(map[string]modernFeaturePlacement)
	for index, name := range []string{"first", "second", "third"} {
		r.f.placed[name] = modernFeaturePlacement{feature: "marker", modifiers: []map[string]any{{"type": "offset", "x": float64(index + 1), "y": float64(0), "z": float64(0)}}}
	}
	return r
}

func TestSelectorsMatchJava263(t *testing.T) {
	// Captured from all four executed Java selector features. Each selected
	// PlacedFeature offsets its real configured feature before placement.
	kinds := []string{"random_selector", "random_boolean_selector", "simple_random_selector", "weighted_random_selector"}
	configs := []map[string]any{
		{"features": []any{map[string]any{"feature": "first", "chance": float64(0.4)}, map[string]any{"feature": "second", "chance": float64(0.6)}}, "default": "third"},
		{"feature_true": "first", "feature_false": "second"},
		{"features": []any{"first", "second", "third"}},
		{"features": []any{map[string]any{"data": "first", "weight": float64(2)}, map[string]any{"data": "second", "weight": float64(3)}, map[string]any{"data": "third", "weight": float64(0)}}},
	}
	for _, tc := range []struct {
		selector  int
		xoroshiro bool
		seed      int64
		chosen    int
		next      int64
	}{
		{0, false, 0, 3, 4437113781045784766}, {0, false, 123, 2, -167885730524958550},
		{0, true, 0, 1, -3694039286755638414}, {0, true, 123, 1, 4378946378199351518},
		{1, false, 0, 1, -3109364765729502342}, {1, false, 123, 1, 4376377975854828753},
		{1, true, 0, 2, -3694039286755638414}, {1, true, 123, 1, 4378946378199351518},
		{2, false, 0, 1, -3109364765729502342}, {2, false, 123, 3, 4376377975854828753},
		{2, true, 0, 3, -3694039286755638414}, {2, true, 123, 2, 4378946378199351518},
		{3, false, 0, 1, -3109364765729502342}, {3, false, 123, 2, 4376377975854828753},
		{3, true, 0, 2, -3694039286755638414}, {3, true, 123, 2, 4378946378199351518},
	} {
		t.Run(fmt.Sprintf("%s/xoro=%v/seed=%d", kinds[tc.selector], tc.xoroshiro, tc.seed), func(t *testing.T) {
			r := selectorTestRegion()
			random := java26.NewLegacyRandom(tc.seed)
			if tc.xoroshiro {
				random = java26.NewXoroshiroRandom(tc.seed)
			}
			if !r.f.placeConfiguredData(r, kinds[tc.selector], configs[tc.selector], featurePos{4, 5, 4}, &random) {
				t.Fatal("selected placed feature did not place")
			}
			for index := range 3 {
				name, _ := r.State(featurePos{int32(5 + index), 5, 4})
				want := "minecraft:air"
				if index+1 == tc.chosen {
					want = "minecraft:stone"
				}
				if name != want {
					t.Fatalf("candidate%d=%s, Java selected%d", index+1, name, tc.chosen)
				}
			}
			if next := random.NextLong(); next != tc.next {
				t.Fatalf("following stream=%d, Java wants%d", next, tc.next)
			}
		})
	}
}

func TestSequenceStopsOnFailureWhileOverlayContinues(t *testing.T) {
	for _, kind := range []string{"sequence", "overlay"} {
		t.Run(kind, func(t *testing.T) {
			r := selectorTestRegion()
			first := r.f.placed["first"]
			first.modifiers = append(first.modifiers, map[string]any{"type": "block_predicate_filter", "predicate": map[string]any{"type": "not", "predicate": map[string]any{"type": "true"}}})
			r.f.placed["first"] = first
			random := java26.NewLegacyRandom(123)
			got := r.f.placeConfiguredData(r, kind, map[string]any{"features": []any{"first", "second"}}, featurePos{4, 5, 4}, &random)
			name, _ := r.State(featurePos{6, 5, 4})
			wantName := "minecraft:air"
			if kind == "overlay" {
				wantName = "minecraft:stone"
			}
			if got != (kind == "overlay") || name != wantName {
				t.Fatalf("%s placed=%v second=%s; wantsecond=%s", kind, got, name, wantName)
			}
		})
	}
}
func TestRandomSelectorStopsAfterSelectedChildFails(t *testing.T) {
	r := selectorTestRegion()
	first := r.f.placed["first"]
	first.modifiers = append(first.modifiers, map[string]any{"type": "block_predicate_filter", "predicate": map[string]any{"type": "not", "predicate": map[string]any{"type": "true"}}})
	r.f.placed["first"] = first

	random := java26.NewLegacyRandom(123)
	wantRandom := java26.NewLegacyRandom(123)
	wantRandom.NextFloat()
	if r.f.placeConfiguredData(r, "random_selector", map[string]any{
		"features": []any{
			map[string]any{"feature": "first", "chance": float64(1)},
			map[string]any{"feature": "second", "chance": float64(1)},
		},
		"default": "third",
	}, featurePos{4, 5, 4}, &random) {
		t.Fatal("selected unsuccessful child returned success")
	}
	for _, p := range []featurePos{{5, 5, 4}, {6, 5, 4}, {7, 5, 4}} {
		if name, _ := r.State(p); name != "minecraft:air" {
			t.Fatalf("later candidate wrote %s at %v after selected child failed", name, p)
		}
	}
	if got, want := random.NextLong(), wantRandom.NextLong(); got != want {
		t.Fatalf("following stream=%d after selected-child failure, want %d", got, want)
	}
}

func TestZeroWeightFeatureSelectorDoesNotDraw(t *testing.T) {
	r := selectorTestRegion()
	random := java26.NewLegacyRandom(123)
	config := map[string]any{"features": []any{map[string]any{"data": "first", "weight": float64(0)}}}
	if r.f.placeConfiguredData(r, "weighted_random_selector", config, featurePos{4, 5, 4}, &random) {
		t.Fatal("zero-weight list placed a feature")
	}
	if next := random.NextLong(); next != -5106534569952410475 {
		t.Fatalf("empty selection consumed random stream: %d", next)
	}
}
