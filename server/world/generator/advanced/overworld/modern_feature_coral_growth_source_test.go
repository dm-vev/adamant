package overworld

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type coralGrowthSource struct {
	ModernFeatureSource
	water, voidAir world.NativeState
}

func (s coralGrowthSource) BaseState(_, y, _ int32) world.NativeState {
	if y < -64 || y > 319 {
		return s.voidAir
	}
	return s.water
}

type coralGrowthFixture struct {
	Cases []struct {
		Algorithm string   `json:"algorithm"`
		Kind      string   `json:"kind"`
		Scenario  string   `json:"scenario"`
		Seed      int64    `json:"seed"`
		Origin    [3]int32 `json:"origin"`
		Placed    bool     `json:"placed"`
		NextLong  int64    `json:"nextLong"`
		Blocks    []struct {
			Pos   [3]int32 `json:"pos"`
			State string   `json:"state"`
		} `json:"blocks"`
	} `json:"cases"`
}

func TestCoralGrowthMatchesSource(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_coral_growth_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture coralGrowthFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	registry := world.DefaultBlockRegistry
	resolve := func(name string) world.NativeState {
		t.Helper()
		state, err := world.ResolveState(registry, world.BlockState{Name: name})
		if err != nil {
			t.Fatalf("resolve %s: %v", name, err)
		}
		return state
	}
	water, stone, voidAir := resolve("minecraft:water"), resolve("minecraft:stone"), resolve("minecraft:void_air")
	for _, tc := range fixture.Cases {
		t.Run(tc.Algorithm+"/"+tc.Kind+"/"+tc.Scenario, func(t *testing.T) {
			chunkPos := world.ChunkPos{tc.Origin[0] >> 4, tc.Origin[2] >> 4}
			c := chunk.New(registry, cube.Range{-64, 319})
			for x := int32(0); x < 16; x++ {
				for z := int32(0); z < 16; z++ {
					for y := max(tc.Origin[1]-10, int32(-64)); y <= min(tc.Origin[1]+12, int32(319)); y++ {
						world.SetNativeState(c, uint8(x), int16(y), uint8(z), water)
					}
				}
			}
			region := newFeatureRegion(&ModernFeatures{source: coralGrowthSource{water: water, voidAir: voidAir}}, c, chunkPos, &world.GenerationResult{})
			region.SetOrigin(chunkPos[0], chunkPos[1])
			origin := featurePos{tc.Origin[0], tc.Origin[1], tc.Origin[2]}
			switch tc.Scenario {
			case "occupied_origin":
				region.Set(origin, stone)
			case "branch_reject":
				for _, direction := range []featurePos{{0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}} {
					for y := int32(1); y <= 3; y++ {
						region.Set(featurePos{origin.x + direction.x, origin.y + y, origin.z + direction.z}, stone)
					}
				}
			}
			initial := make(map[featurePos]world.NativeState, region.overlay.len())
			for pos, state := range region.overlayByPos() {
				initial[pos] = state
			}
			feature := map[string]any{
				"feature": map[string]any{
					"type": "simple_block",
					"to_place": map[string]any{"type": "weighted", "entries": []any{
						map[string]any{"data": "minecraft:brain_coral_block", "weight": float64(2)},
						map[string]any{"data": "minecraft:tube_coral_block", "weight": float64(1)},
						map[string]any{"data": "minecraft:horn_coral_block", "weight": float64(1)},
					}},
				},
				"placement": []any{map[string]any{"type": "block_predicate_filter", "predicate": map[string]any{
					"type": "matching_blocks", "blocks": "minecraft:water",
				}}},
			}
			random := vanilla.NewLegacyRandom(tc.Seed)
			if tc.Algorithm == "xoroshiro" {
				random = vanilla.NewXoroshiroRandom(tc.Seed)
			}
			placed := region.PlaceValue(map[string]any{"type": "coral_" + tc.Kind, "feature": feature}, origin, &random)
			if placed != tc.Placed {
				t.Fatalf("placed=%v; Java source=%v", placed, tc.Placed)
			}
			want := make(map[featurePos]string, len(tc.Blocks))
			for _, block := range tc.Blocks {
				pos := featurePos{block.Pos[0], block.Pos[1], block.Pos[2]}
				want[pos] = block.State
			}
			for pos, native := range region.overlayByPos() {
				if before, ok := initial[pos]; ok && before == native {
					continue
				}
				stateName, _ := region.State(pos)
				if expected, ok := want[pos]; !ok {
					t.Errorf("unexpected coral at %v: %s", pos, stateName)
				} else {
					if stateName != expected {
						t.Errorf("state at %v = %s; Java source=%s", pos, stateName, expected)
					}
					delete(want, pos)
				}
			}
			for pos, state := range want {
				t.Errorf("missing Java coral at %v: %s", pos, state)
			}
			if got := random.NextLong(); got != tc.NextLong {
				t.Errorf("next random long=%d; Java source=%d", got, tc.NextLong)
			}
		})
	}
}
