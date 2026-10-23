package overworld

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type geodeSourceCase struct {
	Name          string              `json:"name"`
	Algorithm     string              `json:"algorithm"`
	Seed          int64               `json:"seed"`
	WorldSeed     int64               `json:"worldSeed"`
	Origin        [3]int32            `json:"origin"`
	Scenario      string              `json:"scenario"`
	Invalid       bool                `json:"invalid"`
	Config        map[string]any      `json:"config,omitempty"`
	Placed        bool                `json:"placed"`
	NextLong      string              `json:"nextLong"`
	States        []string            `json:"states"`
	Ticks         []string            `json:"ticks"`
	Markers       []string            `json:"markers"`
	InitialStates []geodeInitialState `json:"initialStates"`
}

type geodeInitialState struct {
	Offset     [3]int32       `json:"offset"`
	State      string         `json:"state"`
	Properties map[string]any `json:"properties"`
}

type geodeSourceFixture struct {
	Config map[string]any    `json:"config"`
	Cases  []geodeSourceCase `json:"cases"`
}

type geodeSourceTerrain struct {
	ModernFeatureSource
	state     world.NativeState
	voidAir   world.NativeState
	overrides map[featurePos]world.NativeState
}

func (s geodeSourceTerrain) BaseState(x, y, z int32) world.NativeState {
	if y < -64 || y > 319 {
		return s.voidAir
	}
	if state, ok := s.overrides[featurePos{x: x, y: y, z: z}]; ok {
		return state
	}
	return s.state
}

func TestGeodeMatchesJava263SourceFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_geode_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture geodeSourceFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	registry := world.DefaultBlockRegistry
	stone, air, voidAir := geodeResolveState(t, "stone"), geodeResolveState(t, "air"), geodeResolveState(t, "void_air")
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			base := stone
			if tc.Invalid {
				base = air
			}
			overrides := make(map[featurePos]world.NativeState, len(tc.InitialStates))
			for _, initial := range tc.InitialStates {
				pos := featurePos{x: tc.Origin[0] + initial.Offset[0], y: tc.Origin[1] + initial.Offset[1], z: tc.Origin[2] + initial.Offset[2]}
				properties := make(map[string]any, len(initial.Properties))
				for key, value := range initial.Properties {
					if n, ok := value.(float64); ok {
						properties[key] = int32(n)
					} else {
						properties[key] = value
					}
				}
				state := world.BlockState{Name: initial.State, Properties: properties}
				native, err := world.ResolveState(registry, state)
				if err != nil {
					t.Fatalf("resolving initial Source state %s: %v", initial.State, err)
				}
				overrides[pos] = native
			}
			source := geodeSourceTerrain{ModernFeatureSource: modernTerrainSource{}, state: base, voidAir: voidAir, overrides: overrides}
			target := world.ChunkPos{tc.Origin[0] >> 4, tc.Origin[2] >> 4}
			c := chunk.New(registry, cube.Range{-64, 319})
			for dx := int32(-16); dx <= 16; dx++ {
				for dy := int32(-16); dy <= 16; dy++ {
					for dz := int32(-16); dz <= 16; dz++ {
						pos := featurePos{x: tc.Origin[0] + dx, y: tc.Origin[1] + dy, z: tc.Origin[2] + dz}
						if pos.y < -64 || pos.y > 319 || pos.x>>4 != target[0] || pos.z>>4 != target[1] {
							continue
						}
						state := base
						if overridden, ok := overrides[pos]; ok {
							state = overridden
						}
						world.SetNativeState(c, uint8(pos.x&15), int16(pos.y), uint8(pos.z&15), state)
					}
				}
			}
			result := &world.GenerationResult{}
			region := newFeatureRegion(&ModernFeatures{seed: tc.WorldSeed, source: source}, c, target, result)
			region.SetOrigin(target[0], target[1])
			var random java26.Random
			if tc.Algorithm == "legacy" {
				random = java26.NewLegacyRandom(tc.Seed)
			} else {
				random = java26.NewXoroshiroRandom(tc.Seed)
			}
			config := geodeScenarioConfig(fixture.Config, tc.Scenario)
			if tc.Config != nil {
				config = tc.Config
			}
			got := placeModernGeode(region, config, featurePos{x: tc.Origin[0], y: tc.Origin[1], z: tc.Origin[2]}, &random)
			if got != tc.Placed {
				t.Fatalf("placed=%v, Source=%v", got, tc.Placed)
			}
			states := make([]string, 0, region.overlay.len())
			for pos, state := range region.overlayByPos() {
				javaState, err := world.JavaState(registry, state)
				if err != nil {
					t.Fatal(err)
				}
				states = append(states, fmt.Sprintf("%d,%d,%d=%s%s", pos.x, pos.y, pos.z, javaState.Name, geodeProperties(javaState.Properties)))
			}
			sort.Strings(states)
			want := append([]string(nil), tc.States...)
			sort.Strings(want)
			if strings.Join(states, "\n") != strings.Join(want, "\n") {
				for i := 0; i < len(states) && i < len(want); i++ {
					if states[i] != want[i] {
						t.Errorf("state[%d]=%s, Source=%s", i, states[i], want[i])
						break
					}
				}
				t.Errorf("final states differ: got %d states, Source %d", len(states), len(want))
			}
			gotTicks := make([]string, 0, len(result.ScheduledTicks))
			for _, tick := range result.ScheduledTicks {
				gotTicks = append(gotTicks, fmt.Sprintf("tick:BlockPos{x=%d, y=%d, z=%d}:%s:0", tick.Pos[0], tick.Pos[1], tick.Pos[2], tick.Type))
			}
			if strings.Join(gotTicks, "\n") != strings.Join(tc.Ticks, "\n") {
				t.Errorf("scheduled ticks=%v, Source=%v", gotTicks, tc.Ticks)
			}
			wantNext, err := strconv.ParseInt(tc.NextLong, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			if got := random.NextLong(); got != wantNext {
				t.Errorf("nextLong=%d, Source=%d", got, wantNext)
			}
			for pos, state := range overrides {
				for _, initial := range tc.InitialStates {
					if initial.State == "minecraft:bedrock" && pos == (featurePos{x: tc.Origin[0] + initial.Offset[0], y: tc.Origin[1] + initial.Offset[1], z: tc.Origin[2] + initial.Offset[2]}) {
						if got := region.nativeState(pos); got != state {
							t.Errorf("protected Source block at %v changed: got %#v, want %#v", pos, got, state)
						}
					}
				}
			}
			gotMarkers := make([]string, 0)
			for section, packedPositions := range result.PostProcessing {
				for _, packed := range packedPositions {
					x := target[0]*16 + int32(packed&15)
					y := int32(section-4)*16 + int32((packed>>4)&15)
					z := target[1]*16 + int32((packed>>8)&15)
					gotMarkers = append(gotMarkers, fmt.Sprintf("mark:%d,%d,%d", x, y, z))
				}
			}
			wantMarkers := geodeTargetMarkers(tc.Markers, target)
			if strings.Join(gotMarkers, "\n") != strings.Join(wantMarkers, "\n") {
				t.Errorf("postprocessing markers=%v, Source=%v", gotMarkers, wantMarkers)
			}
		})
	}
}
func geodeTargetMarkers(markers []string, target [2]int32) []string {
	type marker struct {
		section int32
		value   string
	}
	owned := make([]marker, 0)
	for _, value := range markers {
		raw, ok := strings.CutPrefix(value, "mark:")
		if !ok {
			panic("malformed Source geode marker")
		}
		parts := strings.Split(raw, ",")
		if len(parts) != 3 {
			panic("malformed Source geode marker position")
		}
		var pos [3]int32
		for i := range pos {
			coordinate, err := strconv.ParseInt(parts[i], 10, 32)
			if err != nil {
				panic(err)
			}
			pos[i] = int32(coordinate)
		}
		if pos[0]>>4 != target[0] || pos[2]>>4 != target[1] || pos[1] < -64 || pos[1] > 319 {
			continue
		}
		owned = append(owned, marker{section: pos[1] >> 4, value: value})
	}
	sort.SliceStable(owned, func(i, j int) bool {
		return owned[i].section < owned[j].section
	})
	result := make([]string, len(owned))
	for i, marker := range owned {
		result[i] = marker.value
	}
	return result
}

func geodeScenarioConfig(source map[string]any, scenario string) map[string]any {
	config := make(map[string]any, len(source))
	for k, v := range source {
		config[k] = v
	}
	blocks := make(map[string]any, len(source["blocks"].(map[string]any)))
	for k, v := range source["blocks"].(map[string]any) {
		blocks[k] = v
	}
	config["blocks"] = blocks
	switch scenario {
	case "default":
	case "invalid":
		config["invalid_blocks_threshold"] = float64(0)
	case "uncracked":
		crack := map[string]any{"generate_crack_chance": float64(0)}
		config["crack"] = crack
	case "alternate":
		config["crack"] = map[string]any{"generate_crack_chance": float64(0)}
		config["use_alternate_layer0_chance"] = float64(0)
		config["use_potential_placements_chance"] = float64(1)
		config["placements_require_layer0_alternate"] = false
	case "fluids":
		blocks["filling_provider"] = map[string]any{"id": "minecraft:cave_air"}
		config["crack"] = map[string]any{"generate_crack_chance": float64(1), "base_crack_size": float64(5)}
	case "weighted":
		blocks["filling_provider"] = map[string]any{"type": "minecraft:weighted", "entries": []any{
			map[string]any{"data": "minecraft:air", "weight": float64(1)},
			map[string]any{"data": "minecraft:magma_block", "weight": float64(3)},
		}}
	default:
		panic("overworld: unsupported geode Source test scenario " + scenario)
	}
	return config
}

func geodeResolveState(t *testing.T, name string) world.NativeState {
	t.Helper()
	state, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: "minecraft:" + name})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func geodeProperties(properties map[string]any) string {
	if len(properties) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(properties))
	for k := range properties {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s=%v", k, properties[k])
	}
	b.WriteByte('}')
	return b.String()
}
