package overworld

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type speleothemSourceBlock struct {
	Pos   string `json:"pos"`
	State string `json:"state"`
}
type speleothemSourceCase struct {
	Name            string         `json:"name"`
	Algorithm       string         `json:"algorithm"`
	Feature         string         `json:"feature"`
	Scene           string         `json:"scene"`
	Seed            int64          `json:"seed"`
	Origin          [3]int32       `json:"origin"`
	Config          map[string]any `json:"config"`
	Placed          bool           `json:"placed"`
	NextLong        string         `json:"nextLong"`
	Initial, States []speleothemSourceBlock
}
type speleothemSourceFixture struct {
	MinY, MaxY int
	Cases      []speleothemSourceCase
}
type speleothemSourceTerrain struct {
	ModernFeatureSource
	air, voidAir world.NativeState
	initial      map[featurePos]world.NativeState
}

func (s speleothemSourceTerrain) BaseState(x, y, z int32) world.NativeState {
	if y < -64 || y > 319 {
		return s.voidAir
	}
	if state, ok := s.initial[featurePos{x, y, z}]; ok {
		return state
	}
	return s.air
}
func buildSpeleothemSourceTerrain(t *testing.T, tc speleothemSourceCase) speleothemSourceTerrain {
	t.Helper()
	resolve := func(name string) world.NativeState {
		id, err := javadata.BlockStateID(name, nil)
		if err != nil {
			t.Fatal(err)
		}
		return world.ResolveStateID(world.DefaultBlockRegistry, id)
	}
	terrain := speleothemSourceTerrain{air: resolve("minecraft:air"), voidAir: resolve("minecraft:void_air"), initial: map[featurePos]world.NativeState{}}
	for _, row := range tc.Initial {
		terrain.initial[speleothemSourcePos(t, row.Pos)] = world.ResolveStateID(world.DefaultBlockRegistry, speleothemSourceStateID(t, row.State))
	}
	return terrain
}
func TestSpeleothemFeaturesMatchJava263Source(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_speleothem_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture speleothemSourceFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			terrain := buildSpeleothemSourceTerrain(t, tc)
			origin := featurePos{tc.Origin[0], tc.Origin[1], tc.Origin[2]}
			target := world.ChunkPos{origin.x >> 4, origin.z >> 4}
			c := chunk.New(world.DefaultBlockRegistry, cube.Range{fixture.MinY, fixture.MaxY})
			for pos, state := range terrain.initial {
				if featureInsideTarget(target, pos) && pos.y >= int32(fixture.MinY) && pos.y <= int32(fixture.MaxY) {
					world.SetNativeState(c, uint8(pos.x&15), int16(pos.y), uint8(pos.z&15), state)
				}
			}
			region := newFeatureRegion(&ModernFeatures{source: terrain}, c, target, &world.GenerationResult{})
			region.SetOrigin(target[0], target[1])
			random := java26.NewLegacyRandom(tc.Seed)
			if tc.Algorithm == "xoroshiro" {
				random = java26.NewXoroshiroRandom(tc.Seed)
			}
			place := placeModernSpeleothemClusterExact
			if tc.Feature == "direct" {
				place = placeModernSpeleothemExact
			}
			if got := place(region, tc.Config, origin, &random); got != tc.Placed {
				t.Errorf("placed=%v, Source=%v", got, tc.Placed)
			}
			want := map[featurePos]uint32{}
			for _, row := range tc.States {
				want[speleothemSourcePos(t, row.Pos)] = speleothemSourceStateID(t, row.State)
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
func speleothemSourcePos(t *testing.T, text string) featurePos {
	t.Helper()
	parts := strings.Split(text, ",")
	if len(parts) != 3 {
		t.Fatalf("invalid Source position %q", text)
	}
	var xyz [3]int32
	for i, part := range parts {
		value, err := strconv.ParseInt(part, 10, 32)
		if err != nil {
			t.Fatal(err)
		}
		xyz[i] = int32(value)
	}
	return featurePos{xyz[0], xyz[1], xyz[2]}
}
func speleothemSourceStateID(t *testing.T, text string) uint32 {
	t.Helper()
	name, raw, hasProperties := strings.Cut(text, "[")
	var properties map[string]any
	if hasProperties {
		properties = map[string]any{}
		for _, entry := range strings.Split(strings.TrimSuffix(raw, "]"), ",") {
			key, value, ok := strings.Cut(entry, "=")
			if !ok {
				t.Fatalf("invalid Source state %q", text)
			}
			properties[key] = value
		}
	}
	id, err := javadata.BlockStateID(name, properties)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
