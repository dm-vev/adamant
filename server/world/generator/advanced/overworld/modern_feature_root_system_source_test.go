package overworld

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

type rootSystemSourceFixture struct {
	Oracle string `json:"oracle"`
	Bounds struct {
		MinY int32 `json:"min_y"`
		MaxY int32 `json:"max_y"`
	} `json:"bounds"`
	Cases []struct {
		Name      string         `json:"name"`
		Algorithm string         `json:"algorithm"`
		Seed      int64          `json:"seed"`
		Origin    [3]int32       `json:"origin"`
		Config    map[string]any `json:"config"`
		Initial   []struct {
			Pos        [3]int32          `json:"pos"`
			State      string            `json:"state"`
			Properties map[string]string `json:"properties"`
		} `json:"initial"`
		Placed   bool  `json:"placed"`
		NextLong int64 `json:"next_long"`
		Blocks   []struct {
			Pos        [3]int32          `json:"pos"`
			State      string            `json:"state"`
			Properties map[string]string `json:"properties"`
		} `json:"blocks"`
	} `json:"cases"`
}

type rootSystemSourceTerrain struct {
	states map[featurePos]world.NativeState
	air    world.NativeState
	void   world.NativeState
}

func (s rootSystemSourceTerrain) BiomeAt(int32, int32, int32) string { return "minecraft:plains" }
func (s rootSystemSourceTerrain) NoiseBiomeAt(int32, int32, int32) string {
	return "minecraft:plains"
}
func (s rootSystemSourceTerrain) BaseHeight(x, z int32, _ bool) int32 {
	height := int32(-64)
	for p, state := range s.states {
		if p.x == x && p.z == z && state != s.air && p.y >= height {
			height = p.y + 1
		}
	}
	return height
}
func (rootSystemSourceTerrain) SeaLevel() int32 { return 63 }
func (s rootSystemSourceTerrain) BaseBlock(x, y, z int32) world.Block {
	state := s.BaseState(x, y, z)
	block, ok := world.BlockByRuntimeID(state.Block)
	if !ok {
		panic("root system fixture: unresolved terrain block")
	}
	return block
}
func (rootSystemSourceTerrain) FeatureBiomeNames() []string { return []string{"minecraft:plains"} }
func (s rootSystemSourceTerrain) BaseState(x, y, z int32) world.NativeState {
	if y < -64 || y > 319 {
		return s.void
	}
	if state, ok := s.states[featurePos{x, y, z}]; ok {
		return state
	}
	return s.air
}
func (rootSystemSourceTerrain) UseLegacyRandomSource() bool { return false }
func (rootSystemSourceTerrain) HasSkyLight() bool           { return true }
func (rootSystemSourceTerrain) GenerationDepth() int32      { return 384 }
func (rootSystemSourceTerrain) FluidTickDelay(string) int32 { return 0 }
func (rootSystemSourceTerrain) PlaceStructureStep(world.ChunkPos, int, structure.ProcessorEnvironment, func(structure.Placement)) {
}

func rootSystemFixtureState(t *testing.T, name string, properties map[string]string) world.NativeState {
	t.Helper()
	props := make(map[string]any, len(properties))
	for key, value := range properties {
		switch value {
		case "true":
			props[key] = true
		case "false":
			props[key] = false
		default:
			if n, err := strconv.ParseInt(value, 10, 32); err == nil {
				props[key] = int32(n)
			} else {
				props[key] = value
			}
		}
	}
	state, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: name, Properties: props})
	if err != nil {
		t.Fatalf("resolve fixture state %s%v: %v", name, properties, err)
	}
	return state
}

func TestRootSystemMatchesSource(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_root_system_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture rootSystemSourceFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	air := rootSystemFixtureState(t, "minecraft:air", nil)
	void := rootSystemFixtureState(t, "minecraft:void_air", nil)
	for _, test := range fixture.Cases {
		t.Run(test.Name, func(t *testing.T) {
			initial := make(map[featurePos]world.NativeState, len(test.Initial))
			for _, block := range test.Initial {
				p := featurePos{block.Pos[0], block.Pos[1], block.Pos[2]}
				initial[p] = rootSystemFixtureState(t, block.State, block.Properties)
			}
			source := rootSystemSourceTerrain{states: initial, air: air, void: void}
			c := chunk.New(world.DefaultBlockRegistry, cube.Range{int(fixture.Bounds.MinY), int(fixture.Bounds.MaxY)})
			region := newFeatureRegion(&ModernFeatures{source: source}, c, world.ChunkPos{}, &world.GenerationResult{})
			region.SetOrigin(0, 0)
			for p, state := range initial {
				if !region.Set(p, state) {
					t.Fatalf("failed to load initial state at %v", p)
				}
			}
			before := make(map[featurePos]world.NativeState, len(initial))
			for p, state := range initial {
				before[p] = state
			}
			var random vanilla.Random
			switch test.Algorithm {
			case "legacy":
				random = vanilla.NewLegacyRandom(test.Seed)
			case "xoroshiro":
				random = vanilla.NewXoroshiroRandom(test.Seed)
			default:
				t.Fatalf("unknown Java RNG %q", test.Algorithm)
			}
			origin := featurePos{test.Origin[0], test.Origin[1], test.Origin[2]}
			if got := placeModernRootSystem(region, test.Config, origin, &random); got != test.Placed {
				t.Fatalf("placed=%v, Java Source=%v", got, test.Placed)
			}
			want := make(map[featurePos]struct{}, len(test.Blocks))
			for _, block := range test.Blocks {
				p := featurePos{block.Pos[0], block.Pos[1], block.Pos[2]}
				want[p] = struct{}{}
				name, properties := region.State(p)
				if len(properties) != len(block.Properties) {
					t.Errorf("property count at %v = %d, Java Source=%d", p, len(properties), len(block.Properties))
				}
				if name != block.State {
					t.Errorf("state at %v = %s, Java Source=%s", p, name, block.State)
				}
				for key, value := range block.Properties {
					if got := fmt.Sprint(properties[key]); got != value {
						t.Errorf("property %s at %v = %s, Java Source=%s", key, p, got, value)
					}
				}
			}
			for p, state := range region.overlayByPos() {
				if old, ok := before[p]; ok && old == state {
					continue
				}
				if _, ok := want[p]; !ok {
					t.Errorf("unexpected placement at %v: %s", p, strings.TrimSpace(fmt.Sprint(state)))
				}
				delete(want, p)
			}
			for p := range want {
				t.Errorf("missing Java Source placement at %v", p)
			}
			if got := random.NextLong(); got != test.NextLong {
				t.Errorf("following nextLong=%d, Java Source=%d", got, test.NextLong)
			}
		})
	}
}
