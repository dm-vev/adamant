package overworld

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type oreExposureSource struct {
	ModernFeatureSource
	stone, air world.NativeState
}

func (s oreExposureSource) BaseState(x, y, z int32) world.NativeState {
	inShell := x >= 4 && x <= 12 && z >= 4 && z <= 12 && y >= 55 && y <= 75
	onShell := x == 4 || x == 12 || z == 4 || z == 12 || y == 55 || y == 75
	if y < 0 || y > 127 || (inShell && onShell) || (x == 5 && y == 64 && z == 8) {
		return s.air
	}
	return s.stone
}

func (oreExposureSource) BaseHeight(int32, int32, bool) int32 { return 128 }

// This controlled fixture is also run through the mapped Java OreFeature.place
// with LegacyRandomSource(1234), BlockMatchTest(stone), and size 12. The cases
// pin its resulting coordinates and post-feature random stream for each
// discard chance; the single air block adjacent to (6,64,8) exposes the
// discard behavior while the size-12 vein exercises overlapping spheres.
func TestModernOreExposureJava263(t *testing.T) {
	registry := world.DefaultBlockRegistry
	stone, err := world.ResolveState(registry, world.BlockState{Name: "minecraft:stone"})
	if err != nil {
		t.Fatal(err)
	}
	air, err := world.ResolveState(registry, world.BlockState{Name: "minecraft:air"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		discard    float64
		positions  string
		nextLong   int64
		nextInt    int32
		nextFloat  uint32
		nextDouble uint64
	}{
		{0, "6,64,8;7,64,8;8,64,7;9,64,7;6,65,8;7,65,8;8,65,7;9,65,7", 355551964551352010, 57, 1049001176, 4605233914583742334},
		{.5, "6,64,8;7,64,8;8,64,7;9,64,7;6,65,8;7,65,8;8,65,7;9,65,7", 8506827441715809314, 41, 1041636048, 4596123227178015140},
		{1, "7,64,8;8,64,7;9,64,7;6,65,8;7,65,8;8,65,7;9,65,7", 355551964551352010, 57, 1049001176, 4605233914583742334},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("discard_%g", tc.discard), func(t *testing.T) {
			source := oreExposureSource{stone: stone, air: air}
			c := chunk.New(registry, cube.Range{0, 127})
			region := newFeatureRegion(&ModernFeatures{source: source}, c, world.ChunkPos{}, &world.GenerationResult{})
			region.SetOrigin(0, 0)
			for y := int32(0); y <= 127; y++ {
				for x := int32(0); x < 16; x++ {
					for z := int32(0); z < 16; z++ {
						p := featurePos{x, y, z}
						if state := source.BaseState(x, y, z); state != air {
							region.Set(p, state)
						}
					}
				}
			}
			config := map[string]any{
				"size":                           float64(12),
				"discard_chance_on_air_exposure": tc.discard,
				"targets": []any{map[string]any{
					"state":  "minecraft:diamond_ore",
					"target": map[string]any{"predicate_type": "minecraft:block_match", "block": "minecraft:stone"},
				}},
			}
			random := java26.NewLegacyRandom(1234)
			placed := placeModernOreRegion(region, config, featurePos{8, 65, 8}, &random)
			if !placed {
				t.Fatal("Java OreFeature returned true for this fixture")
			}
			var positions []string
			for y := int32(0); y <= 127; y++ {
				for x := int32(0); x < 16; x++ {
					for z := int32(0); z < 16; z++ {
						state, _ := region.State(featurePos{x, y, z})
						if normalizeJavaResource(state) == "diamond_ore" {
							positions = append(positions, fmt.Sprintf("%d,%d,%d", x, y, z))
						}
					}
				}
			}
			gotPositions := strings.Join(positions, ";")
			if gotPositions != tc.positions {
				t.Fatalf("ore positions=%s want=%s", gotPositions, tc.positions)
			}
			if got := random.NextLong(); got != tc.nextLong {
				t.Fatalf("nextLong=%d want=%d", got, tc.nextLong)
			}
			if got := random.NextInt(117); got != tc.nextInt {
				t.Fatalf("nextInt(117)=%d want=%d", got, tc.nextInt)
			}
			if got := math.Float32bits(random.NextFloat()); got != tc.nextFloat {
				t.Fatalf("nextFloat bits=%d want=%d", got, tc.nextFloat)
			}
			if got := math.Float64bits(random.NextDouble()); got != tc.nextDouble {
				t.Fatalf("nextDouble bits=%d want=%d", got, tc.nextDouble)
			}
		})
	}
}
