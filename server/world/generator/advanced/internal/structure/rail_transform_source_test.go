package structure

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

func TestRailStateTransformsMatchReference(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_rail_transforms.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Block       string   `json:"block"`
		Shape       string   `json:"shape"`
		Mirror      Mirror   `json:"mirror"`
		Rotation    Rotation `json:"rotation"`
		Transformed string   `json:"transformed"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, source := range cases {
		t.Run(fmt.Sprintf("%s/%s/mirror_%d/rotation_%d", source.Block, source.Shape, source.Mirror, source.Rotation), func(t *testing.T) {
			state := world.BlockState{Name: source.Block, Properties: map[string]any{"shape": source.Shape}}
			got := world.TransformState(state, source.Mirror, source.Rotation)
			if got.Properties["shape"] != source.Transformed {
				t.Fatalf("transformed rail shape=%v, Source=%s", got.Properties["shape"], source.Transformed)
			}
		})
	}
}
