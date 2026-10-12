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

type magmaSourceBlock struct {
	Pos        [3]int32       `json:"pos"`
	State      string         `json:"state"`
	Properties map[string]any `json:"properties"`
}
type magmaSourceCase struct {
	Name         string             `json:"name"`
	Algorithm    string             `json:"algorithm"`
	Seed         int64              `json:"seed"`
	Scenario     string             `json:"scenario"`
	FloorY       int32              `json:"floorY"`
	Origin       [3]int32           `json:"origin"`
	Config       map[string]any     `json:"config"`
	Placed       bool               `json:"placed"`
	RejectWrites bool               `json:"rejectWrites"`
	NextLong     string             `json:"nextLong"`
	Initial      []magmaSourceBlock `json:"initial"`
	States       []magmaSourceBlock `json:"states"`
}
type magmaSourceFixture struct {
	MinY  int               `json:"minY"`
	MaxY  int               `json:"maxY"`
	Cases []magmaSourceCase `json:"cases"`
}
type magmaSourceTerrain struct {
	modernTerrainSource
	floor                 int32
	stone, water, voidAir world.NativeState
	initial               map[featurePos]world.NativeState
}

func (s magmaSourceTerrain) BaseState(x, y, z int32) world.NativeState {
	if y < -64 || y > 319 {
		return s.voidAir
	}
	if n, ok := s.initial[featurePos{x, y, z}]; ok {
		return n
	}
	if y <= s.floor {
		return s.stone
	}
	return s.water
}
func buildMagmaSourceTerrain(t *testing.T, tc magmaSourceCase) magmaSourceTerrain {
	t.Helper()
	resolve := func(name string, props map[string]any) world.NativeState {
		n, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: name, Properties: props})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	s := magmaSourceTerrain{floor: tc.FloorY, stone: resolve("minecraft:stone", nil), water: resolve("minecraft:water", nil), voidAir: resolve("minecraft:void_air", nil), initial: map[featurePos]world.NativeState{}}
	if tc.Scenario == "flowing_column" {
		s.water = resolve("minecraft:water", map[string]any{"level": int32(1)})
	}
	for _, state := range tc.Initial {
		s.initial[featurePos{state.Pos[0], state.Pos[1], state.Pos[2]}] = resolve(state.State, state.Properties)
	}
	return s
}
func TestUnderwaterMagmaMatchesJava263Source(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_underwater_magma_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture magmaSourceFixture
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			source := buildMagmaSourceTerrain(t, tc)
			target := world.ChunkPos{tc.Origin[0] >> 4, tc.Origin[2] >> 4}
			c := chunk.New(world.DefaultBlockRegistry, cube.Range{fixture.MinY, fixture.MaxY})
			for x := uint8(0); x < 16; x++ {
				for z := uint8(0); z < 16; z++ {
					for y := fixture.MinY; y <= fixture.MaxY; y++ {
						world.SetNativeState(c, x, int16(y), z, source.BaseState(target[0]*16+int32(x), int32(y), target[1]*16+int32(z)))
					}
				}
			}
			region := newFeatureRegion(&ModernFeatures{source: source}, c, target, &world.GenerationResult{})
			region.SetOrigin(target[0], target[1])
			if tc.RejectWrites {
				region.SetOrigin(target[0]+3, target[1]+3)
			}
			random := java26.NewLegacyRandom(tc.Seed)
			if tc.Algorithm == "xoroshiro" {
				random = java26.NewXoroshiroRandom(tc.Seed)
			}
			if got := placeModernUnderwaterMagma(region, tc.Config, featurePos{tc.Origin[0], tc.Origin[1], tc.Origin[2]}, &random); got != tc.Placed {
				t.Errorf("placed=%v Source=%v", got, tc.Placed)
			}
			want := make(map[featurePos]uint32, len(tc.States))
			for _, state := range tc.States {
				id, err := javadata.BlockStateID(state.State, state.Properties)
				if err != nil {
					t.Fatal(err)
				}
				want[featurePos{state.Pos[0], state.Pos[1], state.Pos[2]}] = id
			}
			for p, n := range region.overlay {
				expected, ok := want[p]
				if !ok {
					t.Errorf("unexpected Source placement at %v", p)
					continue
				}
				if got := n.CanonicalJavaID(c.BlockRegistry()); got != expected {
					t.Errorf("Source state at %v=%d want=%d", p, got, expected)
				}
				delete(want, p)
			}
			for p, id := range want {
				t.Errorf("missing Source state %d at %v", id, p)
			}
			expected, err := strconv.ParseInt(tc.NextLong, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			if got := random.NextLong(); got != expected {
				t.Errorf("nextLong=%d Source=%d", got, expected)
			}
		})
	}
}
