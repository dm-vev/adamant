package overworld

import (
	"encoding/json"
	"os"
	"testing"
)

func TestBuiltinSurvivalMatchesJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_survival.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name     string
		State    any
		Survives bool
		Blocks   []struct {
			Position [3]int32
			State    any
		}
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			region := countLayerTestRegion()
			origin := featurePos{8, 10, 8}
			for _, block := range tc.Blocks {
				state, ok := featureStateBlock(block.State)
				if !ok {
					t.Fatal("invalid fixture state")
				}
				p := featurePos{origin.x + block.Position[0], origin.y + block.Position[1], origin.z + block.Position[2]}
				native, err := state.ResolveNative(region.c.BlockRegistry())
				if err != nil {
					t.Fatal(err)
				}
				region.SetNativeBlock(p, native, state.NativeBlock)
			}
			predicate := map[string]any{"type": "would_survive", "state": tc.State}
			if got := featurePredicate(region, origin, predicate); got != tc.Survives {
				t.Errorf("survives=%v, executed Java wants %v", got, tc.Survives)
			}
		})
	}
}
