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
)

type multifaceSourceFixture struct {
	MinY  int32                 `json:"minY"`
	MaxY  int32                 `json:"maxY"`
	Cases []multifaceSourceCase `json:"cases"`
}
type multifaceSourceCase struct {
	Name         string              `json:"name"`
	Algorithm    string              `json:"algorithm"`
	Seed         int64               `json:"seed"`
	Scene        int                 `json:"scene"`
	Directions   int                 `json:"directions"`
	SupportKind  int                 `json:"supportKind"`
	Origin       [3]int32            `json:"origin"`
	Placed       bool                `json:"placed"`
	NextLong     string              `json:"nextLong"`
	Markers      []string            `json:"markers"`
	Config       map[string]any      `json:"config"`
	RejectWrites bool                `json:"rejectWrites"`
	AllMarkers   map[string][]string `json:"allMarkers"`
	States       []struct {
		Pos   [3]int32 `json:"pos"`
		State string   `json:"state"`
	} `json:"states"`
}

type multifaceSourceTerrain struct {
	ModernFeatureSource
	air, voidAir, stone world.NativeState
	minY, maxY          int32
	origin              featurePos
	scene               int
}

func (s multifaceSourceTerrain) BaseState(x, y, z int32) world.NativeState {
	if y < s.minY || y > s.maxY {
		return s.voidAir
	}
	p := featurePos{x, y, z}
	if p == (featurePos{s.origin.x, s.origin.y - 1, s.origin.z}) && s.scene == 6 {
		return multifaceSourceState("minecraft:oak_stairs", map[string]any{"facing": "north", "half": "bottom"})
	}
	if p == (featurePos{s.origin.x, s.origin.y - 1, s.origin.z}) && s.scene == 7 {
		return multifaceSourceState("minecraft:oak_fence", map[string]any{"waterlogged": true})
	}
	if s.scene == 15 && p == (featurePos{s.origin.x, s.origin.y - 1, s.origin.z}) {
		return multifaceSourceState("minecraft:glow_lichen", map[string]any{"down": true})
	}
	if s.scene == 15 && p == (featurePos{s.origin.x, s.origin.y - 2, s.origin.z}) {
		return s.stone
	}
	if (s.scene >= 10 && s.scene <= 12 || s.scene == 18) && y == s.origin.y && absFeatureInt(x-s.origin.x)+absFeatureInt(z-s.origin.z) == 1 {
		switch s.scene {
		case 10, 18:
			return s.stone
		case 11:
			return multifaceSourceState("minecraft:water", nil)
		case 12:
			return multifaceSourceState("minecraft:oak_fence", nil)
		}
	}
	if s.scene == 12 && y == s.origin.y-1 {
		if x == s.origin.x && z == s.origin.z {
			return s.stone
		}
		return s.air
	}
	if s.scene == 17 && y == s.origin.y && absFeatureInt(x-s.origin.x)+absFeatureInt(z-s.origin.z) == 1 {
		return multifaceSourceState("minecraft:glow_lichen", map[string]any{"north": true, "waterlogged": true})
	}
	if y == s.origin.y-1 && x >= s.origin.x-10 && x <= s.origin.x+10 && z >= s.origin.z-10 && z <= s.origin.z+10 && s.scene != 12 {
		return s.stone
	}
	switch s.scene {
	case 1:
		if p == s.origin {
			return s.stone
		}
	case 2:
		if p == s.origin {
			return multifaceSourceState("minecraft:glow_lichen", map[string]any{"east": true})
		}
	case 3:
		if p == s.origin {
			return multifaceSourceState("minecraft:water", nil)
		}
	case 4:
		if p == s.origin {
			return multifaceSourceState("minecraft:water", map[string]any{"level": "1"})
		}
	case 5:
		if p == s.origin {
			return multifaceSourceState("minecraft:glow_lichen", map[string]any{"north": true})
		}
	}
	return s.air
}

func multifaceSourceState(name string, properties map[string]any) world.NativeState {
	state, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: name, Properties: properties})
	if err != nil {
		panic(err)
	}
	return state
}

func TestMultifaceGrowthMatchesSource(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_multiface_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture multifaceSourceFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	resolve := func(name string) world.NativeState {
		t.Helper()
		state, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: name})
		if err != nil {
			t.Fatalf("resolve %s: %v", name, err)
		}
		return state
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			origin := featurePos{tc.Origin[0], tc.Origin[1], tc.Origin[2]}
			terrain := multifaceSourceTerrain{air: resolve("minecraft:air"), voidAir: resolve("minecraft:void_air"), stone: resolve("minecraft:stone"), minY: fixture.MinY, maxY: fixture.MaxY, origin: origin, scene: tc.Scene}
			target := world.ChunkPos{origin.x >> 4, origin.z >> 4}
			c := chunk.New(world.DefaultBlockRegistry, cube.Range{int(fixture.MinY), int(fixture.MaxY)})
			for x := max(origin.x-10, target[0]<<4); x <= min(origin.x+10, target[0]<<4+15); x++ {
				for z := max(origin.z-10, target[1]<<4); z <= min(origin.z+10, target[1]<<4+15); z++ {
					if (tc.Scene != 12 || x == origin.x && z == origin.z) && origin.y-1 >= fixture.MinY && origin.y-1 <= fixture.MaxY {
						world.SetNativeState(c, uint8(x&15), int16(origin.y-1), uint8(z&15), terrain.stone)
					}
				}
			}
			if tc.Scene >= 1 && tc.Scene <= 5 {
				world.SetNativeState(c, uint8(origin.x&15), int16(origin.y), uint8(origin.z&15), terrain.BaseState(origin.x, origin.y, origin.z))
			}
			if tc.Scene == 6 || tc.Scene == 7 {
				world.SetNativeState(c, uint8(origin.x&15), int16(origin.y-1), uint8(origin.z&15), terrain.BaseState(origin.x, origin.y-1, origin.z))
			}
			if tc.Scene >= 10 && tc.Scene <= 12 || tc.Scene == 17 || tc.Scene == 18 {
				for _, offset := range [...]featurePos{{x: 0, z: -1}, {x: 1}, {z: 1}, {x: -1}} {
					p := featurePos{origin.x + offset.x, origin.y, origin.z + offset.z}
					world.SetNativeState(c, uint8(p.x&15), int16(p.y), uint8(p.z&15), terrain.BaseState(p.x, p.y, p.z))
				}
			}
			if tc.Scene == 15 {
				candidate := featurePos{origin.x, origin.y - 1, origin.z}
				support := featurePos{origin.x, origin.y - 2, origin.z}
				world.SetNativeState(c, uint8(candidate.x&15), int16(candidate.y), uint8(candidate.z&15), terrain.BaseState(candidate.x, candidate.y, candidate.z))
				world.SetNativeState(c, uint8(support.x&15), int16(support.y), uint8(support.z&15), terrain.stone)
			}
			result := world.GenerationResult{}
			region := newFeatureRegion(&ModernFeatures{source: terrain}, c, target, &result)
			region.SetOrigin(target[0], target[1])
			if tc.RejectWrites {
				region.SetOrigin(target[0]+3, target[1]+3)
			}
			config := tc.Config
			random := vanilla.NewLegacyRandom(tc.Seed)
			if tc.Algorithm == "xoroshiro" {
				random = vanilla.NewXoroshiroRandom(tc.Seed)
			}
			if got := placeModernMultifaceGrowth(region, config, origin, &random); got != tc.Placed {
				t.Errorf("placed=%v, Source=%v", got, tc.Placed)
			}
			want := make(map[featurePos]string, len(tc.States))
			for _, state := range tc.States {
				want[featurePos{state.Pos[0], state.Pos[1], state.Pos[2]}] = state.State
			}
			gotStates := make(map[featurePos]string)
			for x := origin.x - 2; x <= origin.x+2; x++ {
				for y := origin.y - 2; y <= origin.y+2; y++ {
					for z := origin.z - 2; z <= origin.z+2; z++ {
						p := featurePos{x, y, z}
						name, properties := region.State(p)
						if name != "minecraft:glow_lichen" {
							continue
						}
						keys := make([]string, 0, len(properties))
						for key := range properties {
							keys = append(keys, key)
						}
						sort.Strings(keys)
						var state strings.Builder
						state.WriteString(name)
						state.WriteByte('{')
						for i, key := range keys {
							if i != 0 {
								state.WriteString(", ")
							}
							fmt.Fprintf(&state, "%s=%v", key, properties[key])
						}
						state.WriteByte('}')
						gotStates[p] = state.String()
					}
				}
			}
			for p, expected := range want {
				if gotStates[p] != expected {
					t.Errorf("state at %v=%s, Source=%s", p, gotStates[p], expected)
				}
				delete(gotStates, p)
			}
			for p, got := range gotStates {
				t.Errorf("unexpected Source multiface state at %v: %s", p, got)
			}
			var markers []string
			for section, positions := range result.PostProcessing {
				for _, packed := range positions {
					markers = append(markers, fmt.Sprintf("%d:%d", section, packed))
				}
			}
			if fmt.Sprint(markers) != fmt.Sprint(tc.Markers) {
				t.Errorf("postprocessing markers=%v, Source=%v", markers, tc.Markers)
			}
			wantNext, err := strconv.ParseInt(tc.NextLong, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			if got := random.NextLong(); got != wantNext {
				t.Errorf("nextLong=%d, Source=%d", got, wantNext)
			}
		})
	}
}
