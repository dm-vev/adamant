package overworld

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

type heightProviderOracle struct {
	Name                   string
	Provider               map[string]any
	Seed                   int64
	Xoroshiro              bool
	MinY, Height, SeaLevel int32
	Values                 []int32
	Next                   int64
	FirstHeight            int32
	RotationAfterFirst     int
}

func heightProviderCases(t *testing.T) []heightProviderOracle {
	t.Helper()
	data, err := os.ReadFile("testdata/java263_height_providers.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []heightProviderOracle
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	return cases
}
func TestHeightProvidersMatchSource(t *testing.T) {
	for _, row := range heightProviderCases(t) {
		random := vanilla.NewLegacyRandom(row.Seed)
		if row.Xoroshiro {
			random = vanilla.NewXoroshiroRandom(row.Seed)
		}
		for i, want := range row.Values {
			got := sampleHeight(row.Provider, &random, row.MinY, row.MinY+row.Height, row.SeaLevel)
			if got != want {
				t.Fatalf("%s seed=%d xoro=%v minY=%d height=%d sample=%d: got %d want %d", row.Name, row.Seed, row.Xoroshiro, row.MinY, row.Height, i, got, want)
			}
		}
		if got := random.NextLong(); got != row.Next {
			t.Fatalf("%s seed=%d xoro=%v following stream=%d, want %d", row.Name, row.Seed, row.Xoroshiro, got, row.Next)
		}
	}
}
func TestJigsawStartSamplesHeightBeforePoolSelection(t *testing.T) {
	for _, row := range heightProviderCases(t) {
		if row.MinY != -64 || row.Height != 384 || row.Seed != 0 || row.Xoroshiro {
			continue
		}
		switch row.Name {
		case "trial_uniform", "above_bottom", "below_top", "sea_relative":
		default:
			continue
		}
		t.Run(row.Name, func(t *testing.T) {
			config := map[string]any{}
			for key, value := range requireJSON("structure", "trial_chambers") {
				config[key] = value
			}
			config["size"] = float64(1)
			// Isolate height sampling from the trial structure's explicit padding.
			delete(config, "dimension_padding")
			config["start_height"] = row.Provider
			random := vanilla.NewLegacyRandom(row.Seed)
			source := liquidStructureSource{pillarAirSource{ModernFeatureSource: modernTerrainSource{}, air: world.DefaultBlockRegistry.AirRuntimeID()}}
			start, ok, err := buildModernJigsawStructure(modernStructureBuildInput{seed: 0, name: "trial_chambers", chunkX: -1, chunkZ: -17, config: config, source: source, random: &random})
			if err != nil || !ok {
				t.Fatalf("trial start: found=%v err=%v", ok, err)
			}
			piece := start.pieces[0].(modernJigsawPiece).piece
			if got := piece.Bounds.Min[1] + piece.GroundLevelDelta; got != row.FirstHeight {
				t.Errorf("root absolute ground height=%d, source sampled %d", got, row.FirstHeight)
			}
			if piece.Rotation != structure.Rotation(row.RotationAfterFirst) {
				t.Errorf("root rotation=%v, source following height draw=%d", piece.Rotation, row.RotationAfterFirst)
			}
		})
	}
}
