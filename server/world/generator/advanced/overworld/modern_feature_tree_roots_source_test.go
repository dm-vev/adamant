package overworld

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type mangroveRootSourceEnv struct {
	ModernFeatureSource
	air world.NativeState
}

func (s mangroveRootSourceEnv) BaseState(int32, int32, int32) world.NativeState { return s.air }

type mangroveRootSourceFixture struct {
	Oracle string `json:"oracle"`
	Cases  []struct {
		Algorithm string  `json:"algorithm"`
		Seed      int64   `json:"seed"`
		Scenario  string  `json:"scenario"`
		Origin    []int32 `json:"origin"`
		Trunk     []int32 `json:"trunk_origin"`
		Placed    bool    `json:"placed"`
		NextLong  int64   `json:"next_long"`
		Blocks    []struct {
			Pos        []int32           `json:"pos"`
			State      string            `json:"state"`
			Properties map[string]string `json:"properties"`
		} `json:"blocks"`
	} `json:"cases"`
}

func TestMangroveRootsMatchSource(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_mangrove_roots_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture mangroveRootSourceFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	registry := world.DefaultBlockRegistry
	state := func(name string, props map[string]any) world.NativeState {
		t.Helper()
		s, err := world.ResolveState(registry, world.BlockState{Name: "minecraft:" + name, Properties: props})
		if err != nil {
			t.Fatalf("resolve %s: %v", name, err)
		}
		return s
	}
	for _, test := range fixture.Cases {
		t.Run(test.Algorithm+"/"+test.Scenario, func(t *testing.T) {
			c := chunk.New(registry, cube.Range{0, 127})
			air := world.NativeState{Block: registry.AirRuntimeID(), Liquid: registry.AirRuntimeID()}
			region := newFeatureRegion(&ModernFeatures{source: mangroveRootSourceEnv{air: air}}, c, world.ChunkPos{}, &world.GenerationResult{})
			region.SetOrigin(0, 0)
			trunk := featurePos{test.Trunk[0], test.Trunk[1], test.Trunk[2]}
			origin := featurePos{test.Origin[0], test.Origin[1], test.Origin[2]}
			if test.Scenario != "length_boundary" {
				stone := state("stone", nil)
				for x := max(trunk.x-5, -20); x <= min(trunk.x+5, 19); x++ {
					for z := max(trunk.z-5, -20); z <= min(trunk.z+5, 19); z++ {
						if x == trunk.x && z == trunk.z {
							continue
						}
						region.Set(featurePos{x, trunk.y - 1, z}, stone)
					}
				}
			}
			switch test.Scenario {
			case "shallow_water":
				region.Set(featurePos{trunk.x, trunk.y - 1, trunk.z}, state("water", nil))
			case "deep_water":
				region.Set(featurePos{trunk.x, trunk.y - 1, trunk.z}, state("water", map[string]any{"level": 1}))
			case "muddy_substrate":
				region.Set(featurePos{trunk.x, trunk.y - 1, trunk.z}, state("mud", nil))
			case "blocked_column":
				stone := state("stone", nil)
				for y := origin.y; y < trunk.y; y++ {
					region.Set(featurePos{trunk.x, y, trunk.z}, stone)
				}
			}
			maxLength := int32(64)
			if test.Scenario == "length_boundary" {
				maxLength = 1
			}
			root := map[string]any{
				"root_provider": map[string]any{"id": "minecraft:mangrove_roots", "properties": map[string]any{"waterlogged": false}},
				"above_root_placement": map[string]any{
					"above_root_placement_chance": float64(.25),
					"above_root_provider":         map[string]any{"id": "minecraft:moss_carpet"},
				},
				"mangrove_root_placement": map[string]any{
					"can_grow_through":     "#minecraft:mangrove_roots_can_grow_through",
					"muddy_roots_in":       []any{"minecraft:mud", "minecraft:muddy_mangrove_roots"},
					"muddy_roots_provider": map[string]any{"id": "minecraft:muddy_mangrove_roots", "properties": map[string]any{"axis": "y"}},
					"max_root_width":       float64(3), "max_root_length": float64(maxLength), "random_skew_chance": float64(.35),
				},
			}
			var random vanilla.Random
			switch test.Algorithm {
			case "legacy":
				random = vanilla.NewLegacyRandom(test.Seed)
			case "xoroshiro":
				random = vanilla.NewXoroshiroRandom(test.Seed)
			default:
				t.Fatalf("unknown random algorithm %q", test.Algorithm)
			}
			before := make(map[featurePos]world.NativeState, region.overlay.len())
			for p, native := range region.overlayByPos() {
				before[p] = native
			}
			wantBlocks := make(map[featurePos]bool, len(test.Blocks))
			var roots []featurePos
			if got := placeMangroveRoots(region, root, origin, trunk, &random, &roots); got != test.Placed {
				t.Fatalf("placed=%v, Java source=%v", got, test.Placed)
			}
			wantRoots := make([]featurePos, 0, len(test.Blocks))
			for _, expected := range test.Blocks {
				p := featurePos{expected.Pos[0], expected.Pos[1], expected.Pos[2]}
				wantBlocks[p] = true
				name, props := region.State(p)
				if name != expected.State {
					t.Errorf("state at %v = %s, Java source=%s", p, name, expected.State)
				}
				for key, value := range expected.Properties {
					if got := fmt.Sprint(props[key]); got != value {
						t.Errorf("property %s at %v = %s, Java source=%s", key, p, got, value)
					}
				}
				// RootPlacer.placeRoot records above-root placements through the
				// same rootSetter, so decorators see them as root positions.
				if expected.State == "minecraft:mangrove_roots" || expected.State == "minecraft:muddy_mangrove_roots" || expected.State == "minecraft:moss_carpet" {
					wantRoots = append(wantRoots, p)
				}
			}
			for p, native := range region.overlayByPos() {
				if previous, exists := before[p]; exists && previous == native {
					continue
				}
				if !wantBlocks[p] {
					t.Errorf("unexpected mangrove root placement at %v: %v", p, native)
				}
				delete(wantBlocks, p)
			}
			for p := range wantBlocks {
				t.Errorf("missing Java source placement at %v", p)
			}

			if len(roots) != len(wantRoots) {
				t.Errorf("placed root positions=%v, Java source root positions=%v", roots, wantRoots)
			} else {
				for i := range roots {
					if roots[i] != wantRoots[i] {
						t.Errorf("root order[%d]=%v, Java source=%v", i, roots[i], wantRoots[i])
					}
				}
			}
			if got := random.NextLong(); got != test.NextLong {
				t.Errorf("following random long=%d, Java source=%d", got, test.NextLong)
			}
		})
	}
}
