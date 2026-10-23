package overworld

import (
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

func countLayerTestRegion() *featureRegion {
	rangeY := cube.Range{0, 31}
	c := chunk.New(world.DefaultBlockRegistry, rangeY)
	return newFeatureRegion(&ModernFeatures{}, c, world.ChunkPos{}, &world.GenerationResult{})
}

func countLayerNativeState(t *testing.T, name string) world.NativeState {
	t.Helper()
	air := world.DefaultBlockRegistry.AirRuntimeID()
	switch name {
	case "water":
		return world.NativeState{Block: air, Liquid: world.BlockRuntimeID(block.Water{Depth: 8, Still: true})}
	case "bedrock":
		return world.NativeState{Block: world.BlockRuntimeID(block.Bedrock{}), Liquid: air}
	case "nether_quartz_ore":
		return world.NativeState{Block: world.BlockRuntimeID(block.NetherQuartzOre{}), Liquid: air}
	default:
		b, ok := world.BlockByName("minecraft:"+name, nil)
		if !ok {
			t.Fatalf("block %q missing from default registry", name)
		}
		return world.NativeState{Block: world.BlockRuntimeID(b), Liquid: air}
	}
}

func TestPlacementHeightFluidLayersAndNativeAliases(t *testing.T) {
	r := countLayerTestRegion()
	// These columns make the distinction between fluid-aware heightmaps,
	// solid-only ocean floor, and Java/native alias interpretation explicit.
	for y := int32(10); y <= 12; y++ {
		state := countLayerNativeState(t, "air")
		if y == 10 {
			state = countLayerNativeState(t, "stone")
		}
		if y == 11 {
			state = countLayerNativeState(t, "water")
		}
		if !r.Set(featurePos{1, y, 1}, state) {
			t.Fatalf("failed to set fluid column at y=%d", y)
		}
	}
	for y := int32(20); y <= 22; y++ {
		state := countLayerNativeState(t, "air")
		if y == 20 {
			state = countLayerNativeState(t, "nether_quartz_ore")
		}
		if !r.Set(featurePos{2, y, 2}, state) {
			t.Fatalf("failed to set alias column at y=%d", y)
		}
	}
	for _, tc := range []struct {
		x, z int32
		kind string
		want int32
	}{
		{1, 1, "WORLD_SURFACE", 12}, {1, 1, "MOTION_BLOCKING", 12}, {1, 1, "OCEAN_FLOOR", 11},
		{2, 2, "WORLD_SURFACE", 21}, {2, 2, "OCEAN_FLOOR", 21},
	} {
		if got := r.placementHeight(tc.x, tc.z, tc.kind); got != tc.want {
			t.Errorf("placementHeight(%d,%d,%s)=%d, want %d", tc.x, tc.z, tc.kind, got, tc.want)
		}
	}
}

func TestPlacementHeightCacheInvalidatesAfterSet(t *testing.T) {
	r := countLayerTestRegion()
	bottom := featurePos{3, 10, 3}
	top := featurePos{3, 18, 3}
	if !r.Set(bottom, countLayerNativeState(t, "stone")) {
		t.Fatal("failed to set bottom stone")
	}
	if got := r.placementHeight(3, 3, "WORLD_SURFACE"); got != 11 {
		t.Fatalf("initial height=%d, want 11", got)
	}
	if !r.Set(top, countLayerNativeState(t, "stone")) {
		t.Fatal("failed to insert top stone")
	}
	if got := r.placementHeight(3, 3, "WORLD_SURFACE"); got != 19 {
		t.Fatalf("height after insertion=%d, want 19", got)
	}
	if !r.Set(top, countLayerNativeState(t, "air")) {
		t.Fatal("failed to remove top stone")
	}
	if got := r.placementHeight(3, 3, "WORLD_SURFACE"); got != 11 {
		t.Fatalf("height after removal=%d, want 11", got)
	}
}

func TestCountEveryLayerPositionsAcrossStoneAirWaterStack(t *testing.T) {
	r := countLayerTestRegion()
	// Java oracle fixture: minY 0, bedrock at 0, stone at 4 and 12,
	// water at 8, otherwise air; each sampled column is identical.
	for z := range int32(16) {
		for x := range int32(16) {
			for _, layer := range []struct {
				y    int32
				name string
			}{{0, "bedrock"}, {4, "stone"}, {8, "water"}, {12, "stone"}} {
				if !r.Set(featurePos{x, layer.y, z}, countLayerNativeState(t, layer.name)) {
					t.Fatalf("failed to set %s at (%d,%d,%d)", layer.name, x, layer.y, z)
				}
			}
		}
	}
	for _, tc := range []struct {
		name     string
		random   vanilla.Random
		provider any
		want     []featurePos
		next     int64
	}{
		{"legacy/constant", vanilla.NewLegacyRandom(123), float64(2), []featurePos{{11, 13, 3}, {15, 13, 4}, {4, 5, 9}, {9, 5, 4}}, -5237980416576129062},
		{"legacy/uniform", vanilla.NewLegacyRandom(123), map[string]any{"type": "uniform", "min_inclusive": float64(1), "max_inclusive": float64(3)}, []featurePos{{3, 13, 15}, {4, 13, 9}, {12, 5, 9}, {8, 5, 11}, {1, 5, 10}}, 1642239947753385107},
		{"xoroshiro/constant", vanilla.NewXoroshiroRandom(123), float64(2), []featurePos{{6, 13, 12}, {8, 13, 15}, {2, 5, 1}, {14, 5, 9}}, -2644433350256532612},
		{"xoroshiro/uniform", vanilla.NewXoroshiroRandom(123), map[string]any{"type": "uniform", "min_inclusive": float64(1), "max_inclusive": float64(3)}, []featurePos{{12, 13, 8}, {2, 13, 1}, {9, 13, 1}, {6, 5, 15}, {6, 5, 14}}, -9004380913855867372},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := countEveryLayerPositions(r, featurePos{}, tc.provider, &tc.random, nil)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("layer positions=%v, Java wants %v", got, tc.want)
			}
			if next := tc.random.NextLong(); next != tc.next {
				t.Fatalf("following random value=%d, Java wants %d", next, tc.next)
			}
		})
	}
}

func TestBlockPredicateIdentifiersAndOffset(t *testing.T) {
	r := countLayerTestRegion()
	if !r.Set(featurePos{1, 10, 1}, countLayerNativeState(t, "stone")) {
		t.Fatal("failed to set stone")
	}
	for _, blocks := range []any{"minecraft:stone", []any{"minecraft:stone"}, "#minecraft:base_stone_overworld"} {
		predicate := map[string]any{"type": "matching_blocks", "blocks": blocks, "offset": []any{float64(0), float64(-1), float64(0)}}
		if !featurePredicate(r, featurePos{1, 11, 1}, predicate) {
			t.Fatalf("stone did not match offset holder %v", blocks)
		}
		if featurePredicate(r, featurePos{1, 12, 1}, predicate) {
			t.Fatalf("air matched offset holder %v", blocks)
		}
	}
}

// overlayByPos returns the overlay of the region by position.
func (r *featureRegion) overlayByPos() map[featurePos]world.NativeState {
	states := make(map[featurePos]world.NativeState, r.overlay.len())
	for pos, state := range r.overlay.all {
		states[pos] = state
	}
	return states
}
