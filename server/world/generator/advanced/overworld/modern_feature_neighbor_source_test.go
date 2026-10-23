package overworld

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type neighborSourceFixture struct {
	MinY  int                  `json:"minY"`
	MaxY  int                  `json:"maxY"`
	Cases []neighborSourceCase `json:"cases"`
}
type neighborSourceCase struct {
	Name      string         `json:"name"`
	Algorithm string         `json:"algorithm"`
	Seed      int64          `json:"seed"`
	Scenario  string         `json:"scenario"`
	Origin    [3]int32       `json:"origin"`
	Placed    bool           `json:"placed"`
	NextLong  string         `json:"nextLong"`
	Config    map[string]any `json:"config"`
	States    []struct {
		Pos        [3]int32       `json:"pos"`
		State      string         `json:"state"`
		Properties map[string]any `json:"properties"`
	} `json:"states"`
}
type neighborSourceTerrain struct {
	ModernFeatureSource
	air, voidAir world.NativeState
	overrides    map[featurePos]world.NativeState
}

func (s neighborSourceTerrain) BaseState(x, y, z int32) world.NativeState {
	if y < -64 || y > 319 {
		return s.voidAir
	}
	if n, ok := s.overrides[featurePos{x, y, z}]; ok {
		return n
	}
	return s.air
}
func buildNeighborSourceTerrain(t *testing.T, tc neighborSourceCase) neighborSourceTerrain {
	t.Helper()
	resolve := func(name string) world.NativeState {
		n, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: name})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	s := neighborSourceTerrain{air: resolve("minecraft:air"), voidAir: resolve("minecraft:void_air"), overrides: map[featurePos]world.NativeState{}}
	origin := featurePos{tc.Origin[0], tc.Origin[1], tc.Origin[2]}
	switch tc.Scenario {
	case "two_neighbors":
		s.overrides[featurePos{origin.x + 1, origin.y + 1, origin.z}] = resolve("minecraft:netherrack")
	case "occupied":
		stone := resolve("minecraft:stone")
		for x := origin.x - 3; x <= origin.x+3; x++ {
			for y := origin.y - 3; y <= origin.y+4; y++ {
				for z := origin.z - 3; z <= origin.z+3; z++ {
					s.overrides[featurePos{x, y, z}] = stone
				}
			}
		}
	}
	return s
}
func TestRandomNeighborSpreadMatchesSource(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_neighbor_spread_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture neighborSourceFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			terrain := buildNeighborSourceTerrain(t, tc)
			target := world.ChunkPos{tc.Origin[0] >> 4, tc.Origin[2] >> 4}
			c := chunk.New(world.DefaultBlockRegistry, cube.Range{fixture.MinY, fixture.MaxY})
			for p, n := range terrain.overrides {
				if p.x>>4 == target[0] && p.z>>4 == target[1] {
					world.SetNativeState(c, uint8(p.x&15), int16(p.y), uint8(p.z&15), n)
				}
			}
			result := world.GenerationResult{}
			region := newFeatureRegion(&ModernFeatures{source: terrain}, c, target, &result)
			region.SetOrigin(target[0], target[1])
			random := vanilla.NewLegacyRandom(tc.Seed)
			if tc.Algorithm == "xoroshiro" {
				random = vanilla.NewXoroshiroRandom(tc.Seed)
			}
			if got := placeModernRandomNeighborSpread(region, tc.Config, featurePos{tc.Origin[0], tc.Origin[1], tc.Origin[2]}, &random); got != tc.Placed {
				t.Fatalf("placed=%v Source=%v", got, tc.Placed)
			}
			want := make(map[featurePos]uint32, len(tc.States))
			for _, state := range tc.States {
				id, err := gamedata.BlockStateID(state.State, state.Properties)
				if err != nil {
					t.Fatal(err)
				}
				want[featurePos{state.Pos[0], state.Pos[1], state.Pos[2]}] = id
			}
			for p, n := range region.overlayByPos() {
				expected, ok := want[p]
				if !ok {
					t.Errorf("unexpected Source placement at %v", p)
					continue
				}
				if got := n.CanonicalID(c.BlockRegistry()); got != expected {
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
