package javastructure

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
)

func legacyFilterPiece(states ...string) JigsawPiece {
	blocks := make([]Block, len(states))
	for i, name := range states {
		blocks[i] = Block{Pos: [3]int32{int32(i), 0, 0}, State: world.BlockState{Name: name, Properties: javadata.BlockPropertiesWithDefaults(name, nil)}}
	}
	return JigsawPiece{Template: &Template{Size: [3]int32{int32(len(states)), 1, 1}, Palettes: [][]Block{blocks}}, Projection: Rigid}
}

func legacyFilterRule(input, output string) ProcessorSpec {
	return ProcessorSpec{Type: "rule", Config: map[string]any{"rules": []any{map[string]any{
		"input_predicate":    map[string]any{"predicate_type": "minecraft:block_match", "block": input},
		"location_predicate": map[string]any{"predicate_type": "minecraft:always_true"},
		"output_state":       output,
	}}}}
}

func placeLegacyFilterTest(t *testing.T, piece JigsawPiece) Placement {
	t.Helper()
	placed, err := piece.PlaceWithEnvironment(0, Bounds{Min: [3]int32{-128, -128, -128}, Max: [3]int32{128, 128, 128}}, 5, ProcessorEnvironment{BaseHeight: func(x, z int32, oceanFloor bool) int32 { return 80 }})
	if err != nil {
		t.Fatal(err)
	}
	return placed
}

func TestLegacySinglePoolElementFinalBlockIgnore(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_legacy_pool_processors.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name       string   `json:"name"`
		Legacy     bool     `json:"legacy"`
		Terrain    bool     `json:"terrain"`
		Input      string   `json:"input"`
		RuleOutput string   `json:"rule_output"`
		FinalState string   `json:"final_state"`
		Kept       bool     `json:"kept"`
		State      string   `json:"state"`
		Pos        [3]int32 `json:"pos"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/legacy=%v/terrain=%v", tc.Name, tc.Legacy, tc.Terrain), func(t *testing.T) {
			piece := legacyFilterPiece(tc.Input)
			piece.legacySingle = tc.Legacy
			piece.Origin = [3]int32{1, 8, 2}
			piece.Template.Size = [3]int32{3, 4, 3}
			piece.Template.Palettes[0][0].Pos = [3]int32{1, 2, 1}
			if tc.Terrain {
				piece.Projection = TerrainMatching
			}
			if tc.FinalState != "" {
				piece.Template.Palettes[0][0].NBT = map[string]any{"final_state": tc.FinalState}
			}
			if tc.RuleOutput != "" {
				piece.ProcessorSpecs = []ProcessorSpec{legacyFilterRule(tc.Input, tc.RuleOutput)}
			}
			placed := placeLegacyFilterTest(t, piece)
			if !tc.Kept {
				if len(placed.Blocks) != 0 {
					t.Fatalf("source filtered block; placed %+v", placed.Blocks)
				}
			} else if len(placed.Blocks) != 1 || placed.Blocks[0].State.Name != tc.State || placed.Blocks[0].Pos != tc.Pos {
				t.Fatalf("placed %+v; source %s at %v", placed.Blocks, tc.State, tc.Pos)
			}
		})
	}
}

func TestLegacySinglePoolElementLowLevelPlaceRemainsCallerControlled(t *testing.T) {
	piece := legacyFilterPiece("minecraft:air")
	piece.legacySingle = true
	placed, err := piece.Place(0, Bounds{Min: [3]int32{-1, -1, -1}, Max: [3]int32{1, 1, 1}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(placed.Blocks) != 1 || placed.Blocks[0].State.Name != "minecraft:air" {
		t.Fatalf("low-level Place applied implicit pool filtering: %+v", placed.Blocks)
	}
}

func TestLegacySinglePoolElementNestedListKeepsSiblingPolicies(t *testing.T) {
	legacyTemplate := &Template{Size: [3]int32{1, 1, 1}, Palettes: [][]Block{{{State: world.BlockState{Name: "minecraft:air", Properties: javadata.BlockPropertiesWithDefaults("minecraft:air", nil)}}}}}
	singleTemplate := &Template{Size: [3]int32{2, 1, 1}, Palettes: [][]Block{{{Pos: [3]int32{1, 0, 0}, State: world.BlockState{Name: "minecraft:air", Properties: javadata.BlockPropertiesWithDefaults("minecraft:air", nil)}}}}}
	definition := map[string]any{"element_type": "minecraft:list_pool_element", "projection": "rigid", "elements": []any{
		map[string]any{"element_type": "minecraft:legacy_single_pool_element", "location": "unused-legacy", "projection": "rigid"},
		map[string]any{"element_type": "minecraft:list_pool_element", "projection": "rigid", "elements": []any{
			map[string]any{"element_type": "minecraft:single_pool_element", "location": "unused-single", "projection": "rigid"},
		}},
	}}
	decoded, err := decodePoolElement(definition, 1)
	if err != nil {
		t.Fatal(err)
	}
	parts, err := newJigsawPoolRegistry().elementParts(&decoded)
	if err != nil {
		t.Fatal(err)
	}
	parts[0].template = legacyTemplate
	parts[1].template = singleTemplate
	piece := makeJigsawPoolPiece(parts, [3]int32{}, None, &decoded, ApplyWaterlogging)
	placed, err := piece.PlaceWithEnvironment(0, Bounds{Min: [3]int32{-4, -4, -4}, Max: [3]int32{4, 4, 4}}, 0, ProcessorEnvironment{})
	if err != nil {
		t.Fatal(err)
	}
	if len(placed.Blocks) != 1 || placed.Blocks[0].Pos != [3]int32{1, 0, 0} || placed.Blocks[0].State.Name != "minecraft:air" {
		t.Fatalf("mixed nested placement = %+v; want only the ordinary single air block", placed.Blocks)
	}
}

func TestLegacySinglePoolElementDroppedBlocksConsumeNoLootSeed(t *testing.T) {
	piece := legacyFilterPiece("minecraft:chest", "minecraft:chest")
	piece.legacySingle = true
	piece.Template.Palettes[0][0].NBT = map[string]any{"LootTable": "minecraft:chests/simple_dungeon"}
	piece.Template.Palettes[0][1].NBT = map[string]any{"LootTable": "minecraft:chests/simple_dungeon"}
	piece.ProcessorSpecs = []ProcessorSpec{legacyFilterRule("minecraft:chest", "minecraft:air")}
	random := java26.NewLegacyRandom(1234)
	wantSeed := random.NextLong()
	placementRandom := java26.NewLegacyRandom(1234)
	placed, err := piece.PlaceWithEnvironment(0, Bounds{Min: [3]int32{-4, -4, -4}, Max: [3]int32{4, 4, 4}}, 0, ProcessorEnvironment{Random: &placementRandom})
	if err != nil {
		t.Fatal(err)
	}
	if len(placed.Blocks) != 0 {
		t.Fatalf("air-converted loot containers survived: %+v", placed.Blocks)
	}
	if got := placementRandom.NextLong(); got != wantSeed {
		t.Fatalf("discarded chest consumed loot RNG: next draw = %d, want untouched draw %d", got, wantSeed)
	}

	piece.ProcessorSpecs = nil
	placementRandom = java26.NewLegacyRandom(1234)
	placed, err = piece.PlaceWithEnvironment(0, Bounds{Min: [3]int32{-4, -4, -4}, Max: [3]int32{4, 4, 4}}, 0, ProcessorEnvironment{Random: &placementRandom})
	if err != nil {
		t.Fatal(err)
	}
	if len(placed.Blocks) != 2 || placed.Blocks[0].NBT["LootTableSeed"] != wantSeed || placed.Blocks[1].NBT["LootTableSeed"] == wantSeed {
		t.Fatalf("loot seeds after filtering = %+v; first surviving chest must receive first RNG draw %d", placed.Blocks, wantSeed)
	}
}
