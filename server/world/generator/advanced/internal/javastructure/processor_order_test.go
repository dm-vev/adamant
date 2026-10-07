package javastructure

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

func TestRuleProcessorMatchesBeforeStateRotation(t *testing.T) {
	template := &Template{Palettes: [][]Block{{{
		Pos:   [3]int32{1, 0, 0},
		State: world.BlockState{Name: "minecraft:oak_stairs", Properties: map[string]any{"facing": "north", "half": "bottom", "shape": "straight", "waterlogged": false}},
	}}}}
	processor, err := compileRuleProcessor(map[string]any{"rules": []any{map[string]any{
		"input_predicate": map[string]any{"predicate_type": "minecraft:blockstate_match", "block_state": map[string]any{
			"id": "minecraft:oak_stairs", "properties": map[string]any{"facing": "north", "half": "bottom", "shape": "straight", "waterlogged": false},
		}},
		"location_predicate": map[string]any{"predicate_type": "minecraft:always_true"},
		"output_state":       map[string]any{"id": "minecraft:birch_stairs", "properties": map[string]any{"facing": "north", "half": "bottom", "shape": "straight", "waterlogged": false}},
	}}}, ProcessorEnvironment{})
	if err != nil {
		t.Fatal(err)
	}
	placement, err := template.PlaceBlocks(0, [3]int32{}, Clockwise90, NoMirror, Bounds{Min: [3]int32{-8, -8, -8}, Max: [3]int32{8, 8, 8}}, 42, processor)
	if err != nil {
		t.Fatal(err)
	}
	if len(placement.Blocks) != 1 {
		t.Fatalf("placed blocks = %d, want one", len(placement.Blocks))
	}
	got := placement.Blocks[0]
	if got.Pos != [3]int32{0, 0, 1} || got.State.Name != "minecraft:birch_stairs" || got.State.Properties["facing"] != "east" {
		t.Fatalf("placed block = position %v state %#v; want rotated east-facing stair at [0 0 1]", got.Pos, got.State)
	}
}

func TestProcessorsRunBeforeFinalClip(t *testing.T) {
	template := &Template{Palettes: [][]Block{{{Pos: [3]int32{}, State: world.BlockState{Name: "minecraft:stone"}}}}}
	processor := Processor{
		EvaluatesEntirePieceState: true,
		Process: func(_ ProcessContext, block Block) (Block, bool, error) {
			block.Pos[0] = 5
			block.State.Name = "minecraft:gold_block"
			return block, true, nil
		},
	}
	placement, err := template.PlaceBlocks(0, [3]int32{}, None, NoMirror, Bounds{Min: [3]int32{5, 0, 0}, Max: [3]int32{5, 0, 0}}, 0, processor)
	if err != nil {
		t.Fatal(err)
	}
	if len(placement.Blocks) != 1 || placement.Blocks[0].Pos != [3]int32{5, 0, 0} || placement.Blocks[0].State.Name != "minecraft:gold_block" {
		t.Fatalf("placed=%+v; want the processor's moved gold block inside the clip", placement.Blocks)
	}
}

func TestGravityThenHeightRuleUsesMovedPosition(t *testing.T) {
	template := &Template{Palettes: [][]Block{{{
		Pos:   [3]int32{1, 2, 3},
		State: world.BlockState{Name: "minecraft:oak_log", Properties: map[string]any{"axis": "x"}},
	}}}}
	gravity, err := compileGravity(map[string]any{"offset": int32(0), "heightmap": "WORLD_SURFACE_WG"}, ProcessorEnvironment{
		BaseHeight: func(_, _ int32, _ bool) int32 { return 15 },
	})
	if err != nil {
		t.Fatal(err)
	}
	rule, err := compileRuleProcessor(map[string]any{"rules": []any{map[string]any{
		"input_predicate":    map[string]any{"predicate_type": "minecraft:height_match", "min_inclusive": 17, "max_inclusive": 17},
		"location_predicate": map[string]any{"predicate_type": "minecraft:always_true"},
		"output_state":       map[string]any{"id": "minecraft:birch_log", "properties": map[string]any{"axis": "x"}},
	}}}, ProcessorEnvironment{})
	if err != nil {
		t.Fatal(err)
	}
	placement, err := template.PlaceBlocks(0, [3]int32{}, Clockwise90, NoMirror, Bounds{Min: [3]int32{-8, -8, -8}, Max: [3]int32{8, 20, 8}}, 0, gravity, rule)
	if err != nil {
		t.Fatal(err)
	}
	if len(placement.Blocks) != 1 || placement.Blocks[0].Pos != [3]int32{-3, 17, 1} || placement.Blocks[0].State.Name != "minecraft:birch_log" || placement.Blocks[0].State.Properties["axis"] != "z" {
		t.Fatalf("placed result = %+v; want height-matched birch log at [-3 17 1] with rotated Z axis", placement.Blocks)
	}
}

func TestCappedProcessorFinalizesAfterRulesAndCountsChanges(t *testing.T) {
	palette := make([]Block, 9)
	for i := range palette {
		palette[i] = Block{Pos: [3]int32{int32(i), 0, 0}, State: world.BlockState{Name: "minecraft:dirt"}}
	}
	palette[8].State.Name = "minecraft:stone"
	template := &Template{Palettes: [][]Block{palette}}
	rules := func(input, output string) map[string]any {
		return map[string]any{"rules": []any{map[string]any{
			"input_predicate":    map[string]any{"predicate_type": "minecraft:block_match", "block": input},
			"location_predicate": map[string]any{"predicate_type": "minecraft:always_true"},
			"output_state":       output,
		}}}
	}
	for _, cappedFirst := range []bool{true, false} {
		delegate := rules("minecraft:gold_block", "minecraft:diamond_block")
		delegate["processor_type"] = "minecraft:rule"
		capped, err := compileCappedProcessor(map[string]any{"limit": 1, "delegate": delegate}, ProcessorEnvironment{BlockCount: len(palette)})
		if err != nil {
			t.Fatal(err)
		}
		rule, err := compileRuleProcessor(rules("minecraft:stone", "minecraft:gold_block"), ProcessorEnvironment{})
		if err != nil {
			t.Fatal(err)
		}
		processors := []Processor{capped, rule}
		if !cappedFirst {
			processors[0], processors[1] = rule, capped
		}
		placement, err := template.PlaceBlocks(0, [3]int32{}, None, NoMirror, Bounds{Max: [3]int32{8, 0, 0}}, 0, processors...)
		if err != nil {
			t.Fatal(err)
		}
		if len(placement.Blocks) != 9 {
			t.Fatalf("placed=%d, want nine surviving records", len(placement.Blocks))
		}
		for i, block := range placement.Blocks {
			want := "minecraft:dirt"
			if i == 8 {
				want = "minecraft:diamond_block"
			}
			if block.State.Name != want {
				t.Errorf("cappedFirst=%v at %v got=%s want=%s", cappedFirst, block.Pos, block.State.Name, want)
			}
		}
	}
}
