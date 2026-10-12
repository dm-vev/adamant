package overworld

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type lakeSourceCase struct {
	Algorithm  string   `json:"algorithm"`
	Seed       int64    `json:"seed"`
	Fluid      string   `json:"fluid"`
	Scene      int      `json:"scene"`
	Origin     [3]int32 `json:"origin"`
	Placed     bool     `json:"placed"`
	NextLong   string   `json:"nextLong"`
	EventCount int      `json:"eventCount"`
	TickCount  int      `json:"tickCount"`
	Ticks      []string `json:"ticks"`
	PostCount  int      `json:"postCount"`
	Post       []string `json:"post"`
	States     []string `json:"states"`
}
type lakeSourceFixture struct {
	Cases []lakeSourceCase `json:"cases"`
}
type lakeSourceTerrain struct {
	modernTerrainSource
	origin                                 featurePos
	scene                                  int
	stone, air, water, flowingWater, fence world.NativeState
}

func (s lakeSourceTerrain) JavaBiomeAt(int32, int32, int32) string      { return "plains" }
func (s lakeSourceTerrain) JavaNoiseBiomeAt(int32, int32, int32) string { return "plains" }
func (s lakeSourceTerrain) BaseState(x, y, z int32) world.NativeState {
	p := featurePos{x, y, z}
	if s.scene == 1 && p == (featurePos{s.origin.x, s.origin.y, s.origin.z}) {
		return s.stone
	}
	if s.scene == 2 && p == (featurePos{s.origin.x, s.origin.y - 2, s.origin.z}) {
		return s.water
	}
	if s.scene == 3 && p == (featurePos{s.origin.x, s.origin.y - 2, s.origin.z}) {
		return s.fence
	}
	if s.scene == 6 && p == (featurePos{s.origin.x, s.origin.y - 2, s.origin.z}) {
		return s.flowingWater
	}
	if s.scene == 7 && p == (featurePos{s.origin.x, s.origin.y, s.origin.z}) {
		return s.water
	}
	if s.scene == 8 && y == s.origin.y+2 && x >= s.origin.x-8 && x < s.origin.x+8 && z >= s.origin.z-8 && z < s.origin.z+8 {
		return s.fence
	}
	if y < s.origin.y {
		return s.stone
	}
	return s.air
}
func lakeSourceState(t *testing.T, name string) world.NativeState {
	t.Helper()
	state, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: "minecraft:" + name})
	if err != nil {
		t.Fatal(err)
	}
	return state
}
func TestLakeMatchesJava263SourceFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_lake_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture lakeSourceFixture
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	stone, air, water, fence := lakeSourceState(t, "stone"), lakeSourceState(t, "air"), lakeSourceState(t, "water"), lakeSourceState(t, "oak_fence")
	flowingWater, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: "minecraft:water", Properties: map[string]any{"level": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	fence, err = world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: "minecraft:oak_fence", Properties: map[string]any{"waterlogged": "true"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixture.Cases {
		t.Run(fmt.Sprintf("%s/%d/%s/scene%d", tc.Algorithm, tc.Seed, tc.Fluid, tc.Scene), func(t *testing.T) {
			origin := featurePos{tc.Origin[0], tc.Origin[1], tc.Origin[2]}
			source := lakeSourceTerrain{modernTerrainSource: modernTerrainSource{}, origin: origin, scene: tc.Scene, stone: stone, air: air, water: water, flowingWater: flowingWater, fence: fence}
			target := world.ChunkPos{origin.x >> 4, origin.z >> 4}
			c := chunk.New(world.DefaultBlockRegistry, cube.Range{-64, 319})
			for x := uint8(0); x < 16; x++ {
				for z := uint8(0); z < 16; z++ {
					for y := origin.y - 4; y <= origin.y+4; y++ {
						p := featurePos{int32(target[0])*16 + int32(x), y, int32(target[1])*16 + int32(z)}
						world.SetNativeState(c, x, int16(y), z, source.BaseState(p.x, p.y, p.z))
					}
				}
			}
			temperature := 0.8
			biome := modernBiomeFeatures{hasPrecipitation: true}
			if tc.Scene == 7 {
				temperature = 0
			}
			result := &world.GenerationResult{}
			region := newFeatureRegion(&ModernFeatures{source: source, temperatures: map[string]float64{"plains": temperature}, biomes: map[string]modernBiomeFeatures{"plains": biome}}, c, target, result)
			region.SetOrigin(target[0], target[1])
			var random java26.Random
			if tc.Algorithm == "legacy" {
				random = java26.NewLegacyRandom(tc.Seed)
			} else {
				random = java26.NewXoroshiroRandom(tc.Seed)
			}
			provider := func(name string) map[string]any { return map[string]any{"id": "minecraft:" + name} }
			always := map[string]any{"type": "true"}
			never := map[string]any{"type": "false"}
			canPlace, canReplace, canBarrier := always, always, always
			if tc.Scene == 4 {
				canPlace = never
			}
			if tc.Scene == 5 || tc.Scene == 9 {
				canReplace = never
			}
			if tc.Scene == 6 || tc.Scene == 9 {
				canBarrier = never
			}
			barrier := any(provider("stone"))
			if tc.Scene == 9 {
				barrier = map[string]any{"type": "weighted", "entries": []any{map[string]any{"weight": float64(1), "data": provider("stone")}, map[string]any{"weight": float64(1), "data": provider("stone")}}}
			}
			config := map[string]any{"fluid": provider(tc.Fluid), "barrier": barrier, "can_place_feature": canPlace, "can_replace_with_air_or_fluid": canReplace, "can_replace_with_barrier": canBarrier}
			placed := placeModernLake(region, config, origin, &random)
			if placed != tc.Placed {
				t.Fatalf("placed=%v, Source=%v", placed, tc.Placed)
			}
			wantNext, err := strconv.ParseInt(tc.NextLong, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			if got := random.NextLong(); got != wantNext {
				t.Errorf("nextLong=%d, Source=%d", got, wantNext)
			}
			states := make([]string, 0, len(region.overlay))
			for p, state := range region.overlay {
				js, err := world.JavaState(world.DefaultBlockRegistry, state)
				if err != nil {
					t.Fatal(err)
				}
				props := "{}"
				if len(js.Properties) > 0 {
					keys := make([]string, 0, len(js.Properties))
					for k := range js.Properties {
						keys = append(keys, k)
					}
					sort.Strings(keys)
					values := ""
					for i, k := range keys {
						if i > 0 {
							values += ", "
						}
						values += k + "=" + fmt.Sprint(js.Properties[k])
					}
					props = "{" + values + "}"
				}
				states = append(states, fmt.Sprintf("%d,%d,%d=%s%s", p.x, p.y, p.z, js.Name, props))
			}
			sort.Strings(states)
			if len(states) != len(tc.States) {
				t.Fatalf("final states=%d Source=%d", len(states), len(tc.States))
			}
			for i := range states {
				if states[i] != tc.States[i] {
					t.Fatalf("state[%d]=%s Source=%s", i, states[i], tc.States[i])
				}
			}
			actualTicks := make([]string, 0, len(result.ScheduledTicks))
			for _, tick := range result.ScheduledTicks {
				if tick.Kind != chunk.BlockTick || tick.Priority != 0 {
					t.Errorf("unexpected Source tick kind=%d priority=%d", tick.Kind, tick.Priority)
				}
				actualTicks = append(actualTicks, fmt.Sprintf("%d,%d,%d=%s", tick.Pos[0], tick.Pos[1], tick.Pos[2], tick.Type))
			}
			if len(actualTicks) != len(tc.Ticks) {
				t.Errorf("scheduled ticks=%d Source=%d", len(actualTicks), len(tc.Ticks))
			} else {
				for i := range actualTicks {
					if actualTicks[i] != tc.Ticks[i] {
						t.Errorf("tick[%d]=%s Source=%s", i, actualTicks[i], tc.Ticks[i])
						break
					}
				}
			}
			actualPost := make([]string, 0)
			for section, values := range result.PostProcessing {
				for _, value := range values {
					actualPost = append(actualPost, fmt.Sprintf("%d:%d", section, value))
				}
			}
			if len(actualPost) != len(tc.Post) {
				t.Errorf("postprocessing markers=%d Source=%d", len(actualPost), len(tc.Post))
			} else {
				for i := range actualPost {
					if actualPost[i] != tc.Post[i] {
						t.Errorf("marker[%d]=%s Source=%s", i, actualPost[i], tc.Post[i])
						break
					}
				}
			}
			if tc.EventCount == 0 && len(states) != 0 {
				t.Errorf("early failure unexpectedly wrote %d states", len(states))
			}
		})
	}
}
