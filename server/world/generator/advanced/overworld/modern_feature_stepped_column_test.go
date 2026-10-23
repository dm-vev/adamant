package overworld

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
)

type steppedColumnOracle struct {
	Clusters []struct {
		Mode       string
		Xoroshiro  bool
		Seed, Next int64
		Placed     bool
		Blocks     []struct {
			Position [3]int32
			Name     string
		}
	}
	Heights []struct {
		Kind    string
		Y       int32
		Matches bool
	}
}

func readSteppedColumnOracle(t *testing.T) steppedColumnOracle {
	t.Helper()
	data, err := os.ReadFile("testdata/java263_stepped_column.json")
	if err != nil {
		t.Fatal(err)
	}
	var oracle steppedColumnOracle
	if err := json.Unmarshal(data, &oracle); err != nil {
		t.Fatal(err)
	}
	return oracle
}

func TestSteppedColumnsMatchReference(t *testing.T) {
	// Executed Java SteppedColumnClusterFeature, including weighted states,
	// uneven ground, forbidden supports, continuation and exhausted searches.
	for _, tc := range readSteppedColumnOracle(t).Clusters {
		t.Run(fmt.Sprintf("%s/xoro=%v/seed=%d", tc.Mode, tc.Xoroshiro, tc.Seed), func(t *testing.T) {
			r := countLayerTestRegion()
			stone := countLayerNativeState(t, "stone")
			bedrock := countLayerNativeState(t, "bedrock")
			for x := range int32(16) {
				for z := range int32(16) {
					r.Set(featurePos{x, 4, z}, stone)
				}
			}
			r.Set(featurePos{7, 4, 9}, countLayerNativeState(t, "air"))
			r.Set(featurePos{7, 3, 9}, stone)
			r.Set(featurePos{9, 5, 9}, stone)
			r.Set(featurePos{7, 4, 7}, bedrock)
			r.Set(featurePos{8, 7, 9}, bedrock)
			if tc.Mode == "blocked_origin" {
				r.Set(featurePos{8, 4, 8}, bedrock)
			}
			want := r.c.Clone()
			for _, block := range tc.Blocks {
				state, err := world.ResolveState(want.BlockRegistry(), world.BlockState{Name: block.Name})
				if err != nil {
					t.Fatal(err)
				}
				p := block.Position
				want.SetBlock(uint8(p[0]), int16(p[1]), uint8(p[2]), 0, state.Block)
				want.SetBlock(uint8(p[0]), int16(p[1]), uint8(p[2]), 1, state.Liquid)
			}
			columnReach := float64(1)
			if tc.Mode == "zero_reach" {
				columnReach = 0
			}
			config := map[string]any{
				"block": map[string]any{"type": "weighted", "entries": []any{
					map[string]any{"data": "minecraft:basalt", "weight": float64(2)},
					map[string]any{"data": "minecraft:gold_block", "weight": float64(1)},
				}},
				"continue_through": map[string]any{"type": "matching_blocks", "blocks": []any{"minecraft:basalt", "minecraft:gold_block"}},
				"can_replace":      map[string]any{"type": "matching_blocks", "blocks": "minecraft:air"},
				"cannot_place_on":  []any{"minecraft:bedrock"},
				"cluster_reach":    float64(1), "column_count": float64(2), "column_reach": columnReach,
				"height": map[string]any{"type": "uniform", "min_inclusive": float64(2), "max_inclusive": float64(4)},
			}
			random := vanilla.NewLegacyRandom(tc.Seed)
			if tc.Xoroshiro {
				random = vanilla.NewXoroshiroRandom(tc.Seed)
			}
			if placed := r.f.placeConfiguredData(r, "stepped_column_cluster", config, featurePos{8, 5, 8}, &random); placed != tc.Placed {
				t.Errorf("placed=%v, Java wants %v", placed, tc.Placed)
			}
			for y := int16(want.Range().Min()); y <= int16(want.Range().Max()); y++ {
				for x := range uint8(16) {
					for z := range uint8(16) {
						for layer := range uint8(2) {
							if got, expected := r.c.Block(x, y, z, layer), want.Block(x, y, z, layer); got != expected {
								t.Fatalf("state at (%d,%d,%d), layer %d: got RID %d, Java wants RID %d", x, y, z, layer, got, expected)
							}
						}
					}
				}
			}
			if next := random.NextLong(); next != tc.Next {
				t.Errorf("following stream=%d, Java wants %d", next, tc.Next)
			}
		})
	}
}

func TestHeightPredicateMatchesReference(t *testing.T) {
	r := countLayerTestRegion()
	r.f.source = modernTerrainSource{}
	for _, tc := range readSteppedColumnOracle(t).Heights {
		predicate := map[string]any{"type": "height_range", "min_inclusive": map[string]any{"above_bottom": float64(0)}, "max_inclusive": map[string]any{"below_top": float64(0)}}
		if tc.Kind == "sea" {
			predicate["min_inclusive"] = map[string]any{"relative_to_sea_level": float64(-2)}
			predicate["max_inclusive"] = map[string]any{"relative_to_sea_level": float64(2)}
		}
		if got := featurePredicate(r, featurePos{0, tc.Y, 0}, predicate); got != tc.Matches {
			t.Errorf("%s y=%d: got %v, Java wants %v", tc.Kind, tc.Y, got, tc.Matches)
		}
	}
}
