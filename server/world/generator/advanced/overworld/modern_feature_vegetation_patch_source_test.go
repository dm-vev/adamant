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

type vegetationPatchSourceBlock struct {
	Pos   [3]int32 `json:"pos"`
	State string   `json:"state"`
}

type vegetationPatchSourceCase struct {
	Name      string                       `json:"name"`
	Algorithm string                       `json:"algorithm"`
	Seed      int64                        `json:"seed"`
	Origin    [3]int32                     `json:"origin"`
	Config    map[string]any               `json:"config"`
	Placed    bool                         `json:"placed"`
	NextLong  string                       `json:"nextLong"`
	Initial   []vegetationPatchSourceBlock `json:"initial"`
	States    []vegetationPatchSourceBlock `json:"states"`
}

type vegetationPatchSourceFixture struct {
	Oracle string                      `json:"oracle"`
	MinY   int                         `json:"minY"`
	MaxY   int                         `json:"maxY"`
	Cases  []vegetationPatchSourceCase `json:"cases"`
}

type vegetationPatchSourceTerrain struct {
	ModernFeatureSource
	minY, maxY   int32
	air, voidAir world.NativeState
	initial      map[featurePos]world.NativeState
}

func (s vegetationPatchSourceTerrain) BaseState(x, y, z int32) world.NativeState {
	if y < s.minY || y > s.maxY {
		return s.voidAir
	}
	if state, ok := s.initial[featurePos{x, y, z}]; ok {
		return state
	}
	return s.air
}

func (s vegetationPatchSourceTerrain) BaseHeight(x, z int32, _ bool) int32 {
	for y := s.maxY; y >= s.minY; y-- {
		state := s.BaseState(x, y, z)
		name, ok := javadata.BlockStateName(state.CanonicalJavaID(world.DefaultBlockRegistry))
		if ok && name != "minecraft:air" && name != "minecraft:cave_air" && name != "minecraft:void_air" {
			return y + 1
		}
	}
	return s.minY
}

func TestVegetationPatchFeaturesMatchJava263Source(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_vegetation_patch_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture vegetationPatchSourceFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.MinY >= fixture.MaxY || len(fixture.Cases) == 0 {
		t.Fatal("Java vegetation patch fixture has invalid build bounds or no cases")
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			resolve := func(name string) world.NativeState {
				t.Helper()
				id, err := javadata.BlockStateID(name, nil)
				if err != nil {
					t.Fatal(err)
				}
				return world.ResolveStateID(world.DefaultBlockRegistry, id)
			}
			terrain := vegetationPatchSourceTerrain{
				minY: int32(fixture.MinY), maxY: int32(fixture.MaxY),
				air: resolve("minecraft:air"), voidAir: resolve("minecraft:void_air"),
				initial: make(map[featurePos]world.NativeState, len(tc.Initial)),
			}
			for _, row := range tc.Initial {
				pos := featurePos{row.Pos[0], row.Pos[1], row.Pos[2]}
				if pos.y < terrain.minY || pos.y > terrain.maxY {
					t.Fatalf("initial position %v is outside fixture bounds", pos)
				}
				id := speleothemSourceStateID(t, row.State)
				terrain.initial[pos] = world.ResolveStateID(world.DefaultBlockRegistry, id)
			}
			origin := featurePos{tc.Origin[0], tc.Origin[1], tc.Origin[2]}
			target := world.ChunkPos{origin.x >> 4, origin.z >> 4}
			c := chunk.New(world.DefaultBlockRegistry, cube.Range{fixture.MinY, fixture.MaxY})
			for pos, state := range terrain.initial {
				if featureInsideTarget(target, pos) {
					world.SetNativeState(c, uint8(pos.x&15), int16(pos.y), uint8(pos.z&15), state)
				}
			}
			region := newFeatureRegion(&ModernFeatures{source: terrain}, c, target, &world.GenerationResult{})
			region.SetOrigin(target[0], target[1])
			random := java26.NewLegacyRandom(tc.Seed)
			if tc.Algorithm == "xoroshiro" {
				random = java26.NewXoroshiroRandom(tc.Seed)
			} else if tc.Algorithm != "legacy" {
				t.Fatalf("unsupported Source RNG algorithm %q", tc.Algorithm)
			}
			if got := region.PlaceValue(tc.Config, origin, &random); got != tc.Placed {
				t.Errorf("placement=%v, Source=%v", got, tc.Placed)
			}
			want := make(map[featurePos]uint32, len(tc.States))
			for _, row := range tc.States {
				pos := featurePos{row.Pos[0], row.Pos[1], row.Pos[2]}
				want[pos] = speleothemSourceStateID(t, row.State)
			}
			for pos, state := range region.overlay {
				got := state.CanonicalJavaID(c.BlockRegistry())
				initial := terrain.BaseState(pos.x, pos.y, pos.z).CanonicalJavaID(c.BlockRegistry())
				if got == initial {
					continue
				}
				expected, exists := want[pos]
				if !exists {
					name, _ := javadata.BlockStateName(got)
					t.Errorf("unexpected Java state %s at %v", name, pos)
					continue
				}
				if got != expected {
					name, _ := javadata.BlockStateName(got)
					wantName, _ := javadata.BlockStateName(expected)
					t.Errorf("state at %v=%s, Source=%s", pos, name, wantName)
				}
				delete(want, pos)
			}
			for pos, state := range want {
				name, _ := javadata.BlockStateName(state)
				t.Errorf("missing Source state %s at %v", name, pos)
			}
			next, err := strconv.ParseInt(tc.NextLong, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			if got := random.NextLong(); got != next {
				t.Errorf("following RNG=%d, Source=%d", got, next)
			}
		})
	}
}
