package overworld

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type icebergSourceCase struct {
	Algorithm   string `json:"algorithm"`
	Seed        int64  `json:"seed"`
	X           int32  `json:"x"`
	Z           int32  `json:"z"`
	SeaLevel    int32  `json:"seaLevel"`
	Obstacle    int    `json:"obstacle"`
	MainState   string `json:"mainState"`
	Placed      bool   `json:"placed"`
	FinalCount  int    `json:"finalCount"`
	FinalSHA256 string `json:"finalSHA256"`
	NextLong    string `json:"nextLong"`
}

type icebergSourceFixture struct {
	ModernFeatureSource
	seaLevel  int32
	water     world.NativeState
	air       world.NativeState
	obstacles map[featurePos]world.NativeState
}

func (s icebergSourceFixture) SeaLevel() int32 { return s.seaLevel }

func (s icebergSourceFixture) BaseState(x, y, z int32) world.NativeState {
	if state, ok := s.obstacles[featurePos{x: x, y: y, z: z}]; ok {
		return state
	}
	if y <= s.seaLevel {
		return s.water
	}
	return s.air
}

func icebergResolveState(t *testing.T, name string) world.NativeState {
	t.Helper()
	state, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: "minecraft:" + name})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestIcebergMatchesJava263SourceFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_iceberg_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []icebergSourceCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	registry := world.DefaultBlockRegistry
	water, air, stone := icebergResolveState(t, "water"), icebergResolveState(t, "air"), icebergResolveState(t, "stone")
	for _, tc := range fixture.Cases {
		t.Run(fmt.Sprintf("%s/%d/%d/%d/%d", tc.Algorithm, tc.Seed, tc.X, tc.SeaLevel, tc.Obstacle), func(t *testing.T) {
			obstacles := make(map[featurePos]world.NativeState)
			if tc.Obstacle != 0 {
				y := tc.SeaLevel + 2
				if tc.Obstacle == 2 {
					y = tc.SeaLevel - 3
				}
				for dx := int32(-2); dx <= 2; dx++ {
					for dz := int32(-2); dz <= 2; dz++ {
						obstacles[featurePos{x: tc.X + dx, y: y, z: tc.Z + dz}] = stone
					}
				}
			}
			source := icebergSourceFixture{ModernFeatureSource: modernTerrainSource{}, seaLevel: tc.SeaLevel, water: water, air: air, obstacles: obstacles}
			target := world.ChunkPos{tc.X >> 4, tc.Z >> 4}
			c := chunk.New(registry, cube.Range{-64, 319})
			for x := uint8(0); x < 16; x++ {
				for z := uint8(0); z < 16; z++ {
					for y := int32(-64); y <= tc.SeaLevel; y++ {
						world.SetNativeState(c, x, int16(y), z, water)
					}
				}
			}
			for pos := range obstacles {
				if pos.x>>4 == target[0] && pos.z>>4 == target[1] {
					world.SetNativeState(c, uint8(pos.x&15), int16(pos.y), uint8(pos.z&15), stone)
				}
			}
			region := newFeatureRegion(&ModernFeatures{source: source}, c, target, &world.GenerationResult{})
			region.SetOrigin(target[0], target[1])
			var random java26.Random
			if tc.Algorithm == "legacy" {
				random = java26.NewLegacyRandom(tc.Seed)
			} else {
				random = java26.NewXoroshiroRandom(tc.Seed)
			}
			placed := placeModernIceberg(region, map[string]any{"state": tc.MainState}, featurePos{x: tc.X, y: tc.SeaLevel + 17, z: tc.Z}, &random)
			if placed != tc.Placed {
				t.Fatalf("placed=%v, Java Source=%v", placed, tc.Placed)
			}
			states := make([]string, 0, len(region.overlay))
			for pos, state := range region.overlay {
				javaState, err := world.JavaState(registry, state)
				if err != nil {
					t.Fatal(err)
				}
				states = append(states, fmt.Sprintf("%d,%d,%d=%s", pos.x, pos.y, pos.z, javaState.Name))
			}
			sort.Strings(states)
			if len(states) != tc.FinalCount {
				t.Fatalf("final state count=%d, Java Source=%d", len(states), tc.FinalCount)
			}
			hash := sha256.New()
			for _, state := range states {
				fmt.Fprint(hash, state, ";")
			}
			if got := hex.EncodeToString(hash.Sum(nil)); got != tc.FinalSHA256 {
				t.Errorf("final state digest=%s, Java Source=%s", got, tc.FinalSHA256)
			}
			wantNext, err := strconv.ParseInt(tc.NextLong, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			if got := random.NextLong(); got != wantNext {
				t.Errorf("nextLong=%d, Java Source=%d", got, wantNext)
			}
			for pos := range obstacles {
				if got := region.nativeState(pos); got != stone {
					t.Errorf("iceberg overwrote obstructing stone at %v", pos)
				}
			}
		})
	}
}
