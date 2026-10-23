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
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

type fossilSourceFixture struct {
	Oracle string             `json:"oracle"`
	Config fossilSourceConfig `json:"config"`
	Cases  []fossilSourceCase `json:"cases"`
}

type fossilSourceConfig struct {
	Fossils  []any  `json:"fossil_structures"`
	Overlays []any  `json:"overlay_structures"`
	Base     string `json:"fossil_processors"`
	Coal     string `json:"coal_processors"`
	Diamonds string `json:"diamond_processors"`
	Corners  int    `json:"max_empty_corners_allowed"`
}

type fossilSourceInitialState struct {
	Pos   [3]int32 `json:"pos"`
	State string   `json:"state"`
}

type fossilSourceCase struct {
	Name            string                     `json:"name"`
	Algorithm       string                     `json:"algorithm"`
	Seed            int64                      `json:"seed"`
	WorldSeed       int64                      `json:"worldSeed"`
	Origin          [3]int32                   `json:"origin"`
	HeightFloor     int32                      `json:"heightFloor"`
	MinY            int32                      `json:"minY"`
	Scenario        string                     `json:"scenario"`
	Placed          bool                       `json:"placed"`
	NextLong        string                     `json:"nextLong"`
	WriteCount      int                        `json:"writeCount"`
	Rotation        string                     `json:"rotation"`
	Index           int                        `json:"index"`
	TargetY         int32                      `json:"targetY"`
	TargetPos       [3]int32                   `json:"targetPos"`
	CornerPositions [][3]int32                 `json:"cornerPositions"`
	InitialStates   []fossilSourceInitialState `json:"initialStates"`
	States          []string                   `json:"states"`
}

type fossilSourceTerrain struct {
	modernTerrainSource
	floor   int32
	minY    int32
	stone   world.NativeState
	air     world.NativeState
	initial map[featurePos]world.NativeState
}

func (s fossilSourceTerrain) BiomeAt(int32, int32, int32) string      { return "plains" }
func (s fossilSourceTerrain) NoiseBiomeAt(int32, int32, int32) string { return "plains" }
func (s fossilSourceTerrain) BaseHeight(x, _ int32, oceanFloor bool) int32 {
	if oceanFloor && (x%5+5)%5 == 0 {
		return s.floor + 3
	}
	return s.floor
}
func (s fossilSourceTerrain) BaseState(x, y, z int32) world.NativeState {
	if y < s.minY || y > 319 {
		return s.air
	}
	if state, ok := s.initial[featurePos{x, y, z}]; ok {
		return state
	}
	return s.stone
}
func (s fossilSourceTerrain) BaseBlock(x, y, z int32) world.Block {
	b, ok := world.BlockByRuntimeID(s.BaseState(x, y, z).Block)
	if !ok {
		panic("fossil source fixture: unresolved base state")
	}
	return b
}
func (fossilSourceTerrain) PlaceStructureStep(world.ChunkPos, int, structure.ProcessorEnvironment, func(structure.Placement)) {
}

func fossilSourceNativeState(t *testing.T, raw string) world.NativeState {
	t.Helper()
	name, properties := raw, map[string]any{}
	if open := strings.IndexByte(raw, '{'); open >= 0 {
		name = raw[:open]
		for _, property := range strings.Split(strings.TrimSuffix(raw[open+1:], "}"), ",") {
			key, value, ok := strings.Cut(property, "=")
			if !ok {
				t.Fatalf("malformed fossil fixture state %q", raw)
			}
			properties[key] = value
		}
	}
	state, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: name, Properties: properties})
	if err != nil {
		t.Fatalf("resolve fixture state %q: %v", raw, err)
	}
	return state
}

func TestFossilMatchesSourceFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_fossil_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture fossilSourceFixture
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	stone := fossilSourceNativeState(t, "minecraft:stone")
	air := fossilSourceNativeState(t, "minecraft:void_air")
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			origin := featurePos{tc.Origin[0], tc.Origin[1], tc.Origin[2]}
			initial := make(map[featurePos]world.NativeState, len(tc.InitialStates))
			for _, state := range tc.InitialStates {
				initial[featurePos{state.Pos[0], state.Pos[1], state.Pos[2]}] = fossilSourceNativeState(t, state.State)
			}
			source := fossilSourceTerrain{
				modernTerrainSource: modernTerrainSource{}, floor: tc.HeightFloor, minY: tc.MinY,
				stone: stone, air: air, initial: initial,
			}
			target := world.ChunkPos{origin.x >> 4, origin.z >> 4}
			c := chunk.New(world.DefaultBlockRegistry, cube.Range{int(tc.MinY), 319})
			for x := uint8(0); x < 16; x++ {
				for z := uint8(0); z < 16; z++ {
					for y := tc.MinY; y <= 319; y++ {
						p := featurePos{int32(target[0])*16 + int32(x), y, int32(target[1])*16 + int32(z)}
						world.SetNativeState(c, x, int16(y), z, source.BaseState(p.x, p.y, p.z))
					}
				}
			}
			region := newFeatureRegion(&ModernFeatures{seed: tc.WorldSeed, source: source}, c, target, &world.GenerationResult{})
			region.SetOrigin(target[0], target[1])
			var random vanilla.Random
			if tc.Algorithm == "legacy" {
				random = vanilla.NewLegacyRandom(tc.Seed)
			} else {
				random = vanilla.NewXoroshiroRandom(tc.Seed)
			}
			overlayProcessors := fixture.Config.Coal
			if tc.Scenario == "diamonds" {
				overlayProcessors = fixture.Config.Diamonds
			}
			config := map[string]any{
				"fossil_structures":         fixture.Config.Fossils,
				"overlay_structures":        fixture.Config.Overlays,
				"fossil_processors":         fixture.Config.Base,
				"overlay_processors":        overlayProcessors,
				"max_empty_corners_allowed": float64(fixture.Config.Corners),
			}
			placed := placeModernFossil(region, config, origin, &random)
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
			gotStates := make([]string, 0, region.overlay.len())
			for p, state := range region.overlayByPos() {
				js, err := world.CanonicalState(world.DefaultBlockRegistry, state)
				if err != nil {
					t.Fatal(err)
				}
				properties := "{}"
				if len(js.Properties) > 0 {
					keys := make([]string, 0, len(js.Properties))
					for key := range js.Properties {
						keys = append(keys, key)
					}
					sort.Strings(keys)
					values := make([]string, 0, len(keys))
					for _, key := range keys {
						values = append(values, key+"="+fmt.Sprint(js.Properties[key]))
					}
					properties = "{" + strings.Join(values, ",") + "}"
				}
				gotStates = append(gotStates, fmt.Sprintf("%d,%d,%d=%s%s", p.x, p.y, p.z, js.Name, properties))
			}
			sort.Strings(gotStates)
			if len(gotStates) != len(tc.States) {
				t.Errorf("final written states=%d, Source=%d (Source placement calls=%d)", len(gotStates), len(tc.States), tc.WriteCount)
			}
			if fmt.Sprint(gotStates) != fmt.Sprint(tc.States) {
				for i := 0; i < len(gotStates) && i < len(tc.States); i++ {
					if gotStates[i] != tc.States[i] {
						t.Errorf("first differing sorted state at %d: got %s, Source %s", i, gotStates[i], tc.States[i])
						break
					}
				}
			}
		})
	}
}
