package overworld

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type largeDripstoneSourceEnv struct {
	ModernFeatureSource
	air world.NativeState
}

func (s largeDripstoneSourceEnv) BaseState(int32, int32, int32) world.NativeState { return s.air }
func (largeDripstoneSourceEnv) BaseHeight(int32, int32, bool) int32               { return 100 }

type largeDripstoneSourceFixture struct {
	Oracle string `json:"oracle"`
	Bounds struct {
		MinY int `json:"min_y"`
		MaxY int `json:"max_y"`
	} `json:"bounds"`
	Terrain struct {
		FloorY   int32  `json:"floor_y"`
		CeilingY int32  `json:"ceiling_y"`
		Radius   int32  `json:"radius"`
		Block    string `json:"block"`
	} `json:"terrain"`
	Config map[string]any `json:"config"`
	Cases  []struct {
		Algorithm     string  `json:"algorithm"`
		Seed          int64   `json:"seed"`
		Scenario      string  `json:"scenario"`
		Origin        []int32 `json:"origin"`
		Placed        bool    `json:"placed"`
		NextLong      int64   `json:"next_long"`
		CapturedCount int     `json:"captured_count"`
		Blocks        []struct {
			Pos        []int32        `json:"pos"`
			State      string         `json:"state"`
			Properties map[string]any `json:"properties"`
		} `json:"blocks"`
	} `json:"cases"`
}

func largeDripstoneBuildSourceTerrain(region *featureRegion, origin featurePos, radius, floorY, ceilingY int32, stone world.NativeState) {
	for x := origin.x - radius; x <= origin.x+radius; x++ {
		for z := origin.z - radius; z <= origin.z+radius; z++ {
			region.Set(featurePos{x, floorY, z}, stone)
			region.Set(featurePos{x, ceilingY, z}, stone)
		}
	}
}

func TestLargeDripstoneMatchesJava263Source(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_large_dripstone_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture largeDripstoneSourceFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	registry := world.DefaultBlockRegistry
	state := func(name string, properties ...map[string]any) world.NativeState {
		t.Helper()
		var props map[string]any
		if len(properties) != 0 {
			props = properties[0]
		}
		s, err := world.ResolveState(registry, world.BlockState{Name: name, Properties: props})
		if err != nil {
			t.Fatalf("resolve %s: %v", name, err)
		}
		return s
	}
	air := world.NativeState{Block: registry.AirRuntimeID(), Liquid: registry.AirRuntimeID()}
	for _, test := range fixture.Cases {
		t.Run(test.Algorithm+"/"+test.Scenario, func(t *testing.T) {
			c := chunk.New(registry, cube.Range{fixture.Bounds.MinY, fixture.Bounds.MaxY})
			origin := featurePos{test.Origin[0], test.Origin[1], test.Origin[2]}
			target := world.ChunkPos{int32(origin.x >> 4), int32(origin.z >> 4)}
			region := newFeatureRegion(&ModernFeatures{source: largeDripstoneSourceEnv{air: air}}, c, target, &world.GenerationResult{})
			region.SetOrigin(target[0], target[1])
			stone := state(fixture.Terrain.Block)
			floorY, ceilingY := fixture.Terrain.FloorY, fixture.Terrain.CeilingY
			if test.Scenario == "overlap" {
				floorY, ceilingY = 31, 36
			}
			if test.Scenario == "build_min" {
				floorY, ceilingY = -64, -36
			}
			if test.Scenario == "build_max" {
				floorY, ceilingY = 90, 127
			}
			largeDripstoneBuildSourceTerrain(region, origin, fixture.Terrain.Radius, floorY, ceilingY, stone)
			switch test.Scenario {
			case "basic", "negative_edge", "no_wind", "overlap", "direct_set", "tag_set", "build_min", "build_max":
			case "replaceable":
				region.Set(featurePos{origin.x, ceilingY, origin.z}, state("minecraft:dirt"))
			case "water":
				region.Set(origin, state("minecraft:water"))
			case "flowing_water":
				region.Set(origin, state("minecraft:water", map[string]any{"level": int32(1)}))
			case "lava":
				region.Set(featurePos{origin.x, origin.y - 1, origin.z}, state("minecraft:lava"))
			case "rejected":
				region.Set(origin, stone)
			case "shrink":
				region.Set(featurePos{origin.x + 4, floorY, origin.z}, air)
				region.Set(featurePos{origin.x + 4, ceilingY, origin.z}, air)
			default:
				t.Fatalf("unknown scenario %q", test.Scenario)
			}
			before := make(map[featurePos]world.NativeState, region.overlay.len())
			for p, native := range region.overlayByPos() {
				before[p] = native
			}
			var random java26.Random
			switch test.Algorithm {
			case "legacy":
				random = java26.NewLegacyRandom(test.Seed)
			case "xoroshiro":
				random = java26.NewXoroshiroRandom(test.Seed)
			default:
				t.Fatalf("unknown RNG algorithm %q", test.Algorithm)
			}
			config := make(map[string]any, len(fixture.Config))
			for key, value := range fixture.Config {
				config[key] = value
			}
			if test.Scenario == "no_wind" {
				config["min_radius_for_wind"] = float64(100)
			}
			if test.Scenario == "direct_set" {
				config["replaceable_blocks"] = "minecraft:stone"
			}
			if test.Scenario == "tag_set" {
				config["replaceable_blocks"] = "#minecraft:base_stone_overworld"
			}
			if placed := placeModernLargeDripstone(region, config, origin, &random); placed != test.Placed {
				t.Fatalf("placed=%v, Java source=%v", placed, test.Placed)
			}
			want := make(map[featurePos]struct{}, len(test.Blocks))
			for _, expected := range test.Blocks {
				p := featurePos{expected.Pos[0], expected.Pos[1], expected.Pos[2]}
				want[p] = struct{}{}
				name, properties := region.State(p)
				if name != expected.State {
					t.Errorf("state at %v = %s, Java source=%s", p, name, expected.State)
				}
				for key, value := range expected.Properties {
					if got := fmt.Sprint(properties[key]); got != fmt.Sprint(value) {
						t.Errorf("property %s at %v = %s, Java source=%s", key, p, got, value)
					}
				}
			}
			for p, native := range region.overlayByPos() {
				if old, exists := before[p]; exists && old == native {
					continue
				}
				if _, exists := want[p]; !exists {
					t.Errorf("unexpected placement at %v: %v", p, native)
				}
				delete(want, p)
			}
			for p := range want {
				t.Errorf("missing Java source placement at %v", p)
			}
			if got := random.NextLong(); got != test.NextLong {
				t.Errorf("following random long=%d, Java source=%d", got, test.NextLong)
			}
		})
	}
}
