package overworld

import (
	"encoding/json"
	"fmt"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"os"
	"testing"
)

type pillarAirSource struct {
	ModernFeatureSource
	air uint32
}

func (s pillarAirSource) BaseState(_, _, _ int32) world.NativeState {
	return world.NativeState{Block: s.air, Liquid: s.air}
}
func TestPillarFeaturesMatchJava263(t *testing.T) {
	checkFeatureOracle(t, "testdata/java263_pillar.json")
}

func TestMiscFeaturesMatchJava263(t *testing.T) {
	checkFeatureOracle(t, "testdata/java263_misc_features.json")
}

func checkFeatureOracle(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name              string
		Floor             string
		Xoroshiro, Placed bool
		Origin            [3]int32
		Config            map[string]any
		Next              int64
		Blocks            []struct {
			Position [3]int32
			Name     string
		}
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/xoro=%v", tc.Name, tc.Xoroshiro), func(t *testing.T) {
			r := countLayerTestRegion()
			r.f.source = pillarAirSource{air: r.c.BlockRegistry().AirRuntimeID()}
			want := countLayerTestRegion()
			stone, air := countLayerNativeState(t, "stone"), countLayerNativeState(t, "air")
			floor := stone
			if tc.Floor != "" {
				var err error
				floor, err = world.ResolveState(r.c.BlockRegistry(), world.BlockState{Name: tc.Floor})
				if err != nil {
					t.Fatal(err)
				}
			}
			for _, region := range []*featureRegion{r, want} {
				for x := range int32(16) {
					for z := range int32(16) {
						region.Set(featurePos{x, 4, z}, floor)
					}
				}
				region.Set(featurePos{7, 4, 7}, air)
				if tc.Name == "optional_patch" {
					region.Set(featurePos{8, 4, 8}, air)
				}
				if tc.Name == "blocked" {
					region.Set(featurePos{8, 10, 8}, stone)
				}
			}
			for _, block := range tc.Blocks {
				p := block.Position
				state, err := world.ResolveState(want.c.BlockRegistry(), world.BlockState{Name: block.Name})
				if err != nil {
					t.Fatal(err)
				}
				want.Set(featurePos{p[0], p[1], p[2]}, state)
			}
			random := java26.NewLegacyRandom(123)
			if tc.Xoroshiro {
				random = java26.NewXoroshiroRandom(123)
			}
			if placed := r.PlaceValue(tc.Config, featurePos{tc.Origin[0], tc.Origin[1], tc.Origin[2]}, &random); placed != tc.Placed {
				t.Errorf("placed=%v, Java wants %v", placed, tc.Placed)
			}
			for y := int16(r.MinY()); y <= int16(r.MaxY()); y++ {
				for x := range uint8(16) {
					for z := range uint8(16) {
						for layer := range uint8(2) {
							if got, expected := r.c.Block(x, y, z, layer), want.c.Block(x, y, z, layer); got != expected {
								t.Fatalf("state at (%d,%d,%d), layer %d: got RID %d, Java wants RID %d", x, y, z, layer, got, expected)
							}
						}
					}
				}
			}
			if next := random.NextLong(); next != tc.Next {
				t.Errorf("following stream=%d, Java wants %d", next, tc.Next)
			}
		})
	}
}
