package javastructure

import (
	"encoding/json"
	"maps"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
)

func TestRuleProcessorStatesMatchJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_rule_states.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name                     string
		Config                   map[string]any
		Input, Output            json.RawMessage
		Position, OutputPosition [3]int32
		Rotation                 Rotation
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			input, want := oracleRuleState(t, tc.Input), oracleRuleState(t, tc.Output)
			rule, err := compileRuleProcessor(tc.Config, ProcessorEnvironment{})
			if err != nil {
				t.Fatal(err)
			}
			template := Template{Palettes: [][]Block{{{Pos: tc.Position, State: input}}}}
			placed, err := template.PlaceBlocks(0, [3]int32{}, tc.Rotation, NoMirror, Bounds{Min: [3]int32{-20000, -64, -20000}, Max: [3]int32{20000, 320, 20000}}, 0, rule)
			if err != nil {
				t.Fatal(err)
			}
			if len(placed.Blocks) != 1 {
				t.Fatalf("placed=%+v, want one surviving source record", placed.Blocks)
			}
			got := placed.Blocks[0]
			if got.Pos != tc.OutputPosition || got.State.Name != want.Name || !maps.Equal(got.State.Properties, want.Properties) {
				t.Fatalf("got position=%v state=%+v; source position=%v state=%+v", got.Pos, got.State, tc.OutputPosition, want)
			}
		})
	}
}

func oracleRuleState(t *testing.T, raw json.RawMessage) world.BlockState {
	t.Helper()
	var state world.BlockState
	if raw[0] == '"' {
		if err := json.Unmarshal(raw, &state.Name); err != nil {
			t.Fatal(err)
		}
	} else {
		var encoded struct {
			ID         string
			Properties map[string]any
		}
		if err := json.Unmarshal(raw, &encoded); err != nil {
			t.Fatal(err)
		}
		state.Name, state.Properties = encoded.ID, world.DecodeStateProperties(encoded.Properties)
	}
	state.Properties = javadata.BlockPropertiesWithDefaults(state.Name, state.Properties)
	return state
}

func TestRuleProcessorRejectsUnregisteredPredicate(t *testing.T) {
	_, err := compileRuleProcessor(map[string]any{"rules": []any{map[string]any{
		"input_predicate":    map[string]any{"predicate_type": "minecraft:always_false"},
		"location_predicate": map[string]any{"predicate_type": "minecraft:always_true"},
		"output_state":       "minecraft:air",
	}}}, ProcessorEnvironment{})
	if err == nil {
		t.Fatal("unregistered source predicate was silently accepted")
	}
}
