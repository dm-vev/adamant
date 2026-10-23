package overworld

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type snowFreezeSourceBlock struct {
	Pos        [3]int32          `json:"pos"`
	Name       string            `json:"name"`
	Properties map[string]string `json:"properties"`
}

type snowFreezeSourceCase struct {
	Label         string                  `json:"label"`
	Algorithm     string                  `json:"algorithm"`
	Origin        [3]int32                `json:"origin"`
	Ground        int32                   `json:"ground"`
	SeaLevel      int32                   `json:"sea_level"`
	Temperature   float64                 `json:"temperature"`
	Precipitation bool                    `json:"precipitation"`
	Frozen        bool                    `json:"frozen"`
	Scene         string                  `json:"scene"`
	Initial       []snowFreezeSourceBlock `json:"initial"`
	Blocks        []snowFreezeSourceBlock `json:"blocks"`
	Placed        bool                    `json:"placed"`
	NextLong      int64                   `json:"next_long"`
}

type snowFreezeSourceTerrain struct {
	ModernFeatureSource
	states           map[featurePos]world.NativeState
	air, voidAir     world.NativeState
	ground, seaLevel int32
	verticalBoundary bool
}

func (s snowFreezeSourceTerrain) BaseState(x, y, z int32) world.NativeState {
	if y < -64 || y > 319 {
		return s.voidAir
	}
	if state, ok := s.states[featurePos{x, y, z}]; ok {
		return state
	}
	return s.air
}
func (s snowFreezeSourceTerrain) BiomeAt(_, y, _ int32) string {
	if s.verticalBoundary && y <= s.ground {
		return "snow_source_lower"
	}
	return "snow_source"
}
func (s snowFreezeSourceTerrain) NoiseBiomeAt(x, y, z int32) string {
	return s.BiomeAt(x*4, y*4, z*4)
}
func (s snowFreezeSourceTerrain) SeaLevel() int32   { return s.seaLevel }
func (s snowFreezeSourceTerrain) HasSkyLight() bool { return true }

func snowFreezeSourceState(t *testing.T, name string, captured map[string]string) world.NativeState {
	t.Helper()
	properties := make(map[string]any, len(captured))
	for key, value := range captured {
		if value == "true" || value == "false" {
			properties[key] = value == "true"
		} else if number, err := strconv.ParseInt(value, 10, 32); err == nil {
			properties[key] = int32(number)
		} else {
			properties[key] = value
		}
	}
	state, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: name, Properties: properties})
	if err != nil {
		t.Fatalf("resolve Source state %s%v: %v", name, captured, err)
	}
	return state
}

func snowFreezeSourceStateKey(p featurePos, name string, properties map[string]string) string {
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	value := fmt.Sprintf("%d,%d,%d=%s{", p.x, p.y, p.z, name)
	for i, key := range keys {
		if i > 0 {
			value += ","
		}
		value += key + "=" + properties[key]
	}
	return value + "}"
}

func TestSnowAndFreezeMatchesSource(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_snow_freeze_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []snowFreezeSourceCase `json:"cases"`
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	air := snowFreezeSourceState(t, "minecraft:air", nil)
	voidAir := snowFreezeSourceState(t, "minecraft:void_air", nil)
	for _, tc := range fixture.Cases {
		t.Run(tc.Label, func(t *testing.T) {
			origin := featurePos{tc.Origin[0], tc.Origin[1], tc.Origin[2]}
			target := world.ChunkPos{origin.x >> 4, origin.z >> 4}
			source := snowFreezeSourceTerrain{states: make(map[featurePos]world.NativeState, len(tc.Initial)), air: air, voidAir: voidAir, ground: tc.Ground, seaLevel: tc.SeaLevel, verticalBoundary: tc.Scene == "vertical_boundary"}
			c := chunk.New(world.DefaultBlockRegistry, cube.Range{-64, 319})
			for _, entry := range tc.Initial {
				p := featurePos{entry.Pos[0], entry.Pos[1], entry.Pos[2]}
				state := snowFreezeSourceState(t, entry.Name, entry.Properties)
				source.states[p] = state
				if featureInsideTarget(target, p) && p.y >= -64 && p.y <= 319 {
					world.SetNativeState(c, uint8(p.x&15), int16(p.y), uint8(p.z&15), state)
				}
			}
			chunk.LightArea([]*chunk.Chunk{c}, int(target[0]), int(target[1])).Fill()
			for x := int32(0); x < 16; x++ {
				for z := int32(0); z < 16; z++ {
					p := featurePos{origin.x + x, tc.Ground, origin.z + z}
					if !featureInsideTarget(target, p) {
						continue
					}
					for y := max(tc.Ground-1, int32(-64)); y <= min(tc.Ground+2, int32(319)); y++ {
						sub := c.SubChunk(int16(y))
						sub.SetSkyLight(uint8(p.x&15), uint8(y&15), uint8(p.z&15), 15)
						light := uint8(0)
						if tc.Scene == "light_boundary" {
							light = 9
							if x >= 8 {
								light = 10
							}
						}
						sub.SetBlockLight(uint8(p.x&15), uint8(y&15), uint8(p.z&15), light)
					}
				}
			}
			features := &ModernFeatures{source: source, temperatures: map[string]float64{"snow_source": tc.Temperature, "snow_source_lower": .8}, biomes: map[string]modernBiomeFeatures{"snow_source": {frozenTemperature: tc.Frozen, hasPrecipitation: tc.Precipitation}, "snow_source_lower": {hasPrecipitation: true}}}
			region := newFeatureRegion(features, c, target, &world.GenerationResult{})
			region.SetOrigin(target[0], target[1])
			var random vanilla.Random
			if tc.Algorithm == "legacy" {
				random = vanilla.NewLegacyRandom(123)
			} else {
				random = vanilla.NewXoroshiroRandom(123)
			}
			if got := features.placeConfiguredData(region, "freeze_top_layer", nil, origin, &random); got != tc.Placed {
				t.Errorf("placed=%v, Source=%v", got, tc.Placed)
			}
			if got := random.NextLong(); got != tc.NextLong {
				t.Errorf("nextLong=%d, Source=%d", got, tc.NextLong)
			}
			got := make([]string, 0, region.overlay.len())
			for p, state := range region.overlayByPos() {
				canonicalState, err := world.CanonicalState(world.DefaultBlockRegistry, state)
				if err != nil {
					t.Fatal(err)
				}
				properties := make(map[string]string, len(canonicalState.Properties))
				for key, value := range canonicalState.Properties {
					properties[key] = fmt.Sprint(value)
				}
				got = append(got, snowFreezeSourceStateKey(p, canonicalState.Name, properties))
			}
			want := make([]string, 0, len(tc.Blocks))
			for _, entry := range tc.Blocks {
				want = append(want, snowFreezeSourceStateKey(featurePos{entry.Pos[0], entry.Pos[1], entry.Pos[2]}, entry.Name, entry.Properties))
			}
			sort.Strings(got)
			sort.Strings(want)
			if len(got) != len(want) {
				t.Fatalf("changed states=%d, Source=%d", len(got), len(want))
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("state[%d]=%s, Source=%s", i, got[i], want[i])
				}
			}
		})
	}
}
