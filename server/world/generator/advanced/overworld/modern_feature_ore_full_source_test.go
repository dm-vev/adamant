package overworld

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type oreFullSourceBlock struct {
	Pos        [3]int32       `json:"pos"`
	State      string         `json:"state"`
	Properties map[string]any `json:"properties"`
}
type oreFullSourceInitial struct {
	Pos   [3]int32 `json:"pos"`
	State any      `json:"state"`
}
type oreFullSourceCase struct {
	Name, Feature, Algorithm, Scenario string
	Seed                               int64
	Origin                             [3]int32
	Ceiling                            int32
	Background                         any
	RejectWrites                       bool
	Config                             map[string]any
	Placed                             bool
	NextLong                           string
	Initial                            []oreFullSourceInitial
	States                             []oreFullSourceBlock
}
type oreFullSourceFixture struct {
	MinY, MaxY int
	Cases      []oreFullSourceCase
}
type oreFullSourceTerrain struct {
	modernTerrainSource
	scene                                                    string
	ceiling                                                  int32
	background, air, voidAir, slabTop, slabBottom, deepslate world.NativeState
	initial                                                  map[featurePos]world.NativeState
}

func (s oreFullSourceTerrain) BaseState(x, y, z int32) world.NativeState {
	if y < -64 || y > 319 {
		return s.voidAir
	}
	if state, ok := s.initial[featurePos{x, y, z}]; ok {
		return state
	}
	if s.scene == "height_reject" || y >= s.ceiling {
		return s.air
	}
	if s.scene == "slabs" {
		if (x+z)&1 == 0 {
			return s.slabTop
		}
		return s.slabBottom
	}
	if s.scene == "mixed" && (x+z)&1 == 1 {
		return s.deepslate
	}
	return s.background
}
func oreFullRawState(t *testing.T, raw any) world.NativeState {
	t.Helper()
	name, ok := raw.(string)
	var props map[string]any
	if !ok {
		state := raw.(map[string]any)
		name = state["id"].(string)
		props, _ = state["properties"].(map[string]any)
	}
	id, err := javadata.BlockStateID(name, props)
	if err != nil {
		t.Fatal(err)
	}
	return world.ResolveStateID(world.DefaultBlockRegistry, id)
}
func buildOreFullSourceTerrain(t *testing.T, tc oreFullSourceCase) oreFullSourceTerrain {
	t.Helper()
	s := oreFullSourceTerrain{scene: tc.Scenario, ceiling: tc.Ceiling, background: oreFullRawState(t, tc.Background), air: oreFullRawState(t, "minecraft:air"), voidAir: oreFullRawState(t, "minecraft:void_air"), deepslate: oreFullRawState(t, "minecraft:deepslate"), slabTop: oreFullRawState(t, map[string]any{"id": "minecraft:oak_slab", "properties": map[string]any{"type": "top"}}), slabBottom: oreFullRawState(t, map[string]any{"id": "minecraft:oak_slab", "properties": map[string]any{"type": "bottom"}}), initial: map[featurePos]world.NativeState{}}
	for _, row := range tc.Initial {
		s.initial[featurePos{row.Pos[0], row.Pos[1], row.Pos[2]}] = oreFullRawState(t, row.State)
	}
	return s
}
func TestOreFeaturesMatchFullJava263Source(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_ore_full_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture oreFullSourceFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			terrain := buildOreFullSourceTerrain(t, tc)
			origin := featurePos{tc.Origin[0], tc.Origin[1], tc.Origin[2]}
			target := world.ChunkPos{origin.x >> 4, origin.z >> 4}
			c := chunk.New(world.DefaultBlockRegistry, cube.Range{fixture.MinY, fixture.MaxY})
			for x := uint8(0); x < 16; x++ {
				for z := uint8(0); z < 16; z++ {
					for y := fixture.MinY; y <= fixture.MaxY; y++ {
						world.SetNativeState(c, x, int16(y), z, terrain.BaseState(target[0]*16+int32(x), int32(y), target[1]*16+int32(z)))
					}
				}
			}
			region := newFeatureRegion(&ModernFeatures{source: terrain}, c, target, &world.GenerationResult{})
			region.SetOrigin(target[0], target[1])
			if tc.RejectWrites {
				region.SetOrigin(target[0]+3, target[1]+3)
			}
			random := java26.NewLegacyRandom(tc.Seed)
			if tc.Algorithm == "xoroshiro" {
				random = java26.NewXoroshiroRandom(tc.Seed)
			}
			place := placeModernOreRegion
			if tc.Feature == "scattered" {
				place = placeModernScatteredOre
			}
			if got := place(region, tc.Config, origin, &random); got != tc.Placed {
				t.Errorf("placed=%v, Source=%v", got, tc.Placed)
			}
			want := map[featurePos]uint32{}
			for _, row := range tc.States {
				id, err := javadata.BlockStateID(row.State, row.Properties)
				if err != nil {
					t.Fatal(err)
				}
				want[featurePos{row.Pos[0], row.Pos[1], row.Pos[2]}] = id
			}
			for pos, state := range region.overlayByPos() {
				got := state.CanonicalJavaID(c.BlockRegistry())
				if got == terrain.BaseState(pos.x, pos.y, pos.z).CanonicalJavaID(c.BlockRegistry()) {
					continue
				}
				expected, exists := want[pos]
				if !exists {
					t.Errorf("unexpected Source state %d at %v", got, pos)
					continue
				}
				if got != expected {
					t.Errorf("state at %v=%d, Source=%d", pos, got, expected)
				}
				delete(want, pos)
			}
			for pos, state := range want {
				t.Errorf("missing Source state %d at %v", state, pos)
			}
			next, err := strconv.ParseInt(tc.NextLong, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			if got := random.NextLong(); got != next {
				t.Errorf("nextLong=%d, Source=%d", got, next)
			}
		})
	}
}
