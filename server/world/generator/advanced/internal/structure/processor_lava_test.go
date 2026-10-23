package structure

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/world"
)

func TestLavaSubmergedProcessorMatchesReference(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	data, err := os.ReadFile("testdata/java263_lava_submerged.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Submerged     bool
		Input, Output json.RawMessage
	}
	if err = json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	for index, row := range rows {
		bases := []world.Block{block.Netherrack{}}
		if row.Submerged {
			bases = []world.Block{block.Lava{Depth: 8, Still: true}, block.Lava{Depth: 1, Still: false}}
		}
		for _, base := range bases {
			name, _ := base.EncodeBlock()
			t.Run(fmt.Sprintf("%d/%s", index, name), func(t *testing.T) {
				input, want := oracleRuleState(t, row.Input), oracleRuleState(t, row.Output)
				processors, err := LoadProcessors(nil, []ProcessorSpec{{Type: "lava_submerged_block"}}, ProcessorEnvironment{BaseBlock: func(int32, int32, int32) world.Block { return base }})
				if err != nil {
					t.Fatal(err)
				}
				template := Template{Palettes: [][]Block{{{State: input}}}}
				placed, err := template.PlaceBlocks(0, [3]int32{}, None, NoMirror, Bounds{Min: [3]int32{}, Max: [3]int32{}}, 0, processors...)
				if err != nil {
					t.Fatal(err)
				}
				if len(placed.Blocks) != 1 {
					t.Fatalf("placed=%v, source retains one block", placed.Blocks)
				}
				actual := placed.Blocks[0].State
				if actual.Name != want.Name || !maps.Equal(gamedata.BlockPropertiesWithDefaults(actual.Name, actual.Properties), want.Properties) {
					t.Fatalf("actual=%+v source=%+v", actual, want)
				}
			})
		}
	}
}
