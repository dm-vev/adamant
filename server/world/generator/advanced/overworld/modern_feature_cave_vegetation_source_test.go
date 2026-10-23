package overworld

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type sourceCavePlacementFixture struct {
	Scans []struct {
		OriginY   int32      `json:"originY"`
		Step      int32      `json:"step"`
		Positions [][3]int32 `json:"positions"`
	} `json:"scans"`
	Config   map[string]any `json:"config"`
	Placed   bool           `json:"placed"`
	NextLong int64          `json:"nextLong"`
	Blocks   []struct {
		Pos   [3]int32         `json:"pos"`
		State world.BlockState `json:"state"`
	} `json:"blocks"`
}

func loadSourceCavePlacement(t *testing.T) sourceCavePlacementFixture {
	t.Helper()
	data, err := os.ReadFile("testdata/java263_cave_placement.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture sourceCavePlacementFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func TestEnvironmentScanBuildHeightMatchesReference(t *testing.T) {
	fixture := loadSourceCavePlacement(t)
	for _, scan := range fixture.Scans {
		t.Run(fmt.Sprintf("y%d/step%d", scan.OriginY, scan.Step), func(t *testing.T) {
			c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
			source := pillarAirSource{ModernFeatureSource: modernTerrainSource{}, air: c.BlockRegistry().AirRuntimeID()}
			r := newFeatureRegion(&ModernFeatures{source: source}, c, world.ChunkPos{}, &world.GenerationResult{})
			origin := featurePos{8, scan.OriginY, 8}
			r.Set(origin, countLayerNativeState(t, "stone"))
			direction := "up"
			if scan.Step < 0 {
				direction = "down"
			}
			modifier := map[string]any{
				"direction_of_search": direction,
				"target_condition":    map[string]any{"type": "matching_blocks", "blocks": "minecraft:air"},
				"max_steps":           float64(1),
			}
			got := environmentScanPlacement(r, []featurePos{origin}, modifier, int32(c.Range().Min()), int32(c.Range().Max())+1)
			want := make([]featurePos, len(scan.Positions))
			for i, pos := range scan.Positions {
				want[i] = featurePos{pos[0], pos[1], pos[2]}
			}
			if !slices.Equal(got, want) {
				t.Errorf("scan positions=%v, source=%v", got, want)
			}
		})
	}
}

func TestWaterloggedVegetationPatchWaterlogsGroundCellReference(t *testing.T) {
	fixture := loadSourceCavePlacement(t)
	r := templateFluidTestRegion()
	for x := int32(7); x <= 9; x++ {
		for z := int32(7); z <= 9; z++ {
			r.Set(featurePos{x, 63, z}, countLayerNativeState(t, "stone"))
			r.Set(featurePos{x, 64, z}, countLayerNativeState(t, "clay"))
		}
	}
	random := vanilla.NewLegacyRandom(0)
	if placed := r.PlaceValue(fixture.Config, featurePos{8, 65, 8}, &random); placed != fixture.Placed {
		t.Fatalf("patch placement=%v, source=%v", placed, fixture.Placed)
	}
	for _, expected := range fixture.Blocks {
		want, err := world.ResolveState(r.c.BlockRegistry(), expected.State)
		if err != nil {
			t.Fatal(err)
		}
		pos := featurePos{expected.Pos[0], expected.Pos[1], expected.Pos[2]}
		if got := r.nativeState(pos); got != want {
			actual, err := world.CanonicalState(r.c.BlockRegistry(), got)
			if err != nil {
				t.Fatal(err)
			}
			t.Errorf("block at %v=%v, source=%v", pos, actual, expected.State)
		}
	}
	if got := random.NextLong(); got != fixture.NextLong {
		t.Errorf("following RNG=%d, source=%d", got, fixture.NextLong)
	}
}
