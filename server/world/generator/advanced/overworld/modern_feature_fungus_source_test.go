package overworld

import (
	"crypto/sha256"
	"fmt"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"math"
	"testing"
)

type fungusSourceFixture struct {
	ModernFeatureSource
	air world.NativeState
}

func (s fungusSourceFixture) BaseState(int32, int32, int32) world.NativeState { return s.air }
func (fungusSourceFixture) JavaGenerationDepth() int32                        { return 128 }
func TestHugeFungusPreservesNonReplaceableLeavesJava263(t *testing.T) {
	// Executed HugeFungusFeature.place, LegacyRandomSource(1234), origin (8,65,8).
	// Oak leaves obstruct both the stem and hat; the explicit replacement predicate
	// permits shroomlight only in the stem. The source preserves every leaf.
	registry := world.DefaultBlockRegistry
	resolve := func(name string) world.NativeState {
		state, err := world.ResolveState(registry, world.BlockState{Name: name})
		if err != nil {
			t.Fatal(err)
		}
		return state
	}
	air, nylium, leaves := resolve("minecraft:air"), resolve("minecraft:warped_nylium"), resolve("minecraft:oak_leaves")
	c := chunk.New(registry, cube.Range{0, 127})
	region := newFeatureRegion(&ModernFeatures{source: fungusSourceFixture{air: air}}, c, world.ChunkPos{}, &world.GenerationResult{})
	region.SetOrigin(0, 0)
	for x := int32(0); x < 16; x++ {
		for z := int32(0); z < 16; z++ {
			region.Set(featurePos{x, 64, z}, nylium)
		}
	}
	var protected []featurePos
	for x := int32(4); x <= 12; x++ {
		for z := int32(4); z <= 12; z++ {
			protected = append(protected, featurePos{x, 75, z})
		}
	}
	protected = append(protected, featurePos{8, 66, 8})
	for _, pos := range protected {
		region.Set(pos, leaves)
	}
	region.Set(featurePos{8, 67, 8}, resolve("minecraft:shroomlight"))
	config := map[string]any{"valid_base_block": "minecraft:warped_nylium", "stem_state": "minecraft:warped_stem", "hat_state": "minecraft:warped_wart_block", "decor_state": "minecraft:shroomlight", "replaceable_blocks": map[string]any{"type": "minecraft:matching_blocks", "blocks": []any{"minecraft:shroomlight"}}, "planted": false}
	random := java26.NewLegacyRandom(1234)
	if !placeModernHugeFungus(region, config, featurePos{8, 65, 8}, &random) {
		t.Fatal("source placed the fungus")
	}
	for _, pos := range protected {
		if got := region.nativeState(pos); got != leaves {
			t.Errorf("fungus replaced protected leaves at %v", pos)
		}
	}
	hash := sha256.New()
	for y := int32(0); y < 128; y++ {
		for x := int32(0); x < 16; x++ {
			for z := int32(0); z < 16; z++ {
				name, _ := region.State(featurePos{x, y, z})
				switch name {
				case "minecraft:air", "minecraft:warped_nylium", "minecraft:oak_leaves":
					continue
				}
				fmt.Fprintf(hash, "%d,%d,%d=%s;", x, y, z, name)
			}
		}
	}
	if got := fmt.Sprintf("%x", hash.Sum(nil)); got != "54414b5b93a961b0407022370e2a32c000ccee923cca3518e523b3bcbc9adcf3" {
		t.Errorf("generated fungus digest=%s, want 54414b5b93a961b0407022370e2a32c000ccee923cca3518e523b3bcbc9adcf3", got)
	}
	if got := random.NextLong(); got != 6347477978988174296 {
		t.Errorf("nextLong=%d, want 6347477978988174296", got)
	}
	if got := random.NextInt(117); got != 82 {
		t.Errorf("nextInt=%d, want 82", got)
	}
	if got := math.Float32bits(random.NextFloat()); got != 1037887880 {
		t.Errorf("nextFloat bits=%d, want 1037887880", got)
	}
	if got := math.Float64bits(random.NextDouble()); got != 4603073671910505395 {
		t.Errorf("nextDouble bits=%d, want 4603073671910505395", got)
	}
}
