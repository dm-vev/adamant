package world

import (
	"encoding/json"
	"maps"
	"os"
	"testing"
)

func TestParseJavaBlockStateMatchesJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_block_arguments.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Input string
		OK          bool
		State       json.RawMessage
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			got, err := ParseJavaBlockState(tc.Input)
			if !tc.OK {
				if err == nil {
					t.Fatalf("source rejects %q; got %+v", tc.Input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("source accepts %q: %v", tc.Input, err)
			}
			var want BlockState
			if tc.State[0] == '"' {
				if err := json.Unmarshal(tc.State, &want.Name); err != nil {
					t.Fatal(err)
				}
			} else {
				var state struct {
					ID         string
					Properties map[string]any
				}
				if err := json.Unmarshal(tc.State, &state); err != nil {
					t.Fatal(err)
				}
				want.Name, want.Properties = state.ID, DecodeStateProperties(state.Properties)
			}
			if got.Name != want.Name || !maps.Equal(got.Properties, want.Properties) {
				t.Fatalf("input=%q\ngot %+v\nsource %+v", tc.Input, got, want)
			}
		})
	}
}
