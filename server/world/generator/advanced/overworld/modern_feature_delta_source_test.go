package overworld

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type deltaSourceFixture struct {
	MinY  int               `json:"minY"`
	MaxY  int               `json:"maxY"`
	Cases []deltaSourceCase `json:"cases"`
}

type deltaSourceBlock struct {
	Pos   [3]int32 `json:"pos"`
	State string   `json:"state"`
}

type deltaSourceCase struct {
	Name      string             `json:"name"`
	Algorithm string             `json:"algorithm"`
	Seed      int64              `json:"seed"`
	Origin    [3]int32           `json:"origin"`
	Config    map[string]any     `json:"config"`
	Placed    bool               `json:"placed"`
	NextLong  string             `json:"nextLong"`
	Initial   []deltaSourceBlock `json:"initial"`
	States    []deltaSourceBlock `json:"states"`
}

type deltaSourceTerrain struct {
	ModernFeatureSource
	air, voidAir world.NativeState
	states       map[featurePos]world.NativeState
}

func (s deltaSourceTerrain) BaseState(x, y, z int32) world.NativeState {
	if y < -64 || y > 319 {
		return s.voidAir
	}
	if state, ok := s.states[featurePos{x, y, z}]; ok {
		return state
	}
	return s.air
}

func deltaSourceState(t *testing.T, text string) world.NativeState {
	t.Helper()
	return world.ResolveStateID(world.DefaultBlockRegistry, speleothemSourceStateID(t, text))
}

func buildDeltaSourceTerrain(t *testing.T, tc deltaSourceCase) deltaSourceTerrain {
	t.Helper()
	s := deltaSourceTerrain{
		air:     deltaSourceState(t, "minecraft:air"),
		voidAir: deltaSourceState(t, "minecraft:void_air"),
		states:  make(map[featurePos]world.NativeState, len(tc.Initial)),
	}
	for _, row := range tc.Initial {
		s.states[featurePos{row.Pos[0], row.Pos[1], row.Pos[2]}] = deltaSourceState(t, row.State)
	}
	return s
}

func TestDeltaFeatureMatchesSource(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_delta_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture deltaSourceFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			terrain := buildDeltaSourceTerrain(t, tc)
			target := world.ChunkPos{tc.Origin[0] >> 4, tc.Origin[2] >> 4}
			c := chunk.New(world.DefaultBlockRegistry, cube.Range{fixture.MinY, fixture.MaxY})
			for p, state := range terrain.states {
				if p.y >= int32(fixture.MinY) && p.y <= int32(fixture.MaxY) && p.x>>4 == target[0] && p.z>>4 == target[1] {
					world.SetNativeState(c, uint8(p.x&15), int16(p.y), uint8(p.z&15), state)
				}
			}
			region := newFeatureRegion(&ModernFeatures{source: terrain}, c, target, &world.GenerationResult{})
			region.SetOrigin(target[0], target[1])
			random := vanilla.NewLegacyRandom(tc.Seed)
			if tc.Algorithm == "xoroshiro" {
				random = vanilla.NewXoroshiroRandom(tc.Seed)
			}
			origin := featurePos{tc.Origin[0], tc.Origin[1], tc.Origin[2]}
			if got := placeModernDelta(region, tc.Config, origin, &random); got != tc.Placed {
				t.Fatalf("placed=%v Source=%v", got, tc.Placed)
			}
			want := make(map[featurePos]uint32, len(tc.States))
			for _, row := range tc.States {
				state := deltaSourceState(t, row.State)
				want[featurePos{row.Pos[0], row.Pos[1], row.Pos[2]}] = state.CanonicalID(c.BlockRegistry())
			}
			for p, state := range region.overlayByPos() {
				expected, ok := want[p]
				if !ok {
					t.Errorf("unexpected placement at %v", p)
					continue
				}
				if got := state.CanonicalID(c.BlockRegistry()); got != expected {
					t.Errorf("state at %v=%d want=%d", p, got, expected)
				}
				delete(want, p)
			}
			if len(want) != 0 {
				t.Errorf("missing Source placements: %v", want)
			}
			if got, expected := strconv.FormatInt(random.NextLong(), 10), tc.NextLong; got != expected {
				t.Fatalf("nextLong=%s Source=%s", got, expected)
			}
		})
	}
}
