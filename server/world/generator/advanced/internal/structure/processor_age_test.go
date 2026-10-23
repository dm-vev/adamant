package structure

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
)

func TestBlockAgeProcessorMatchesReference(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_block_age.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Mossiness     float32
		Position      [3]int32
		Input, Output json.RawMessage
	}
	if err = json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	for index, row := range rows {
		t.Run(fmt.Sprintf("%d", index), func(t *testing.T) {
			input, want := oracleRuleState(t, row.Input), oracleRuleState(t, row.Output)
			random := vanilla.NewXoroshiroRandom(991)
			expectedRandom := random
			processors, err := LoadProcessors(nil, []ProcessorSpec{{Type: "block_age", Config: map[string]any{"mossiness": row.Mossiness}}}, ProcessorEnvironment{Random: &random})
			if err != nil {
				t.Fatal(err)
			}
			template := Template{Palettes: [][]Block{{{State: input}}}}
			placed, err := template.PlaceBlocks(0, row.Position, None, NoMirror, Bounds{Min: row.Position, Max: row.Position}, 0, processors...)
			if err != nil {
				t.Fatal(err)
			}
			if len(placed.Blocks) != 1 {
				t.Fatalf("placed=%v, source retains one block", placed.Blocks)
			}
			actual := placed.Blocks[0].State
			if actual.Name != want.Name || !maps.Equal(gamedata.BlockPropertiesWithDefaults(actual.Name, actual.Properties), want.Properties) {
				t.Fatalf("position=%v mossiness=%v actual=%+v source=%+v", row.Position, row.Mossiness, actual, want)
			}
			if actual, want := random.NextLong(), expectedRandom.NextLong(); actual != want {
				t.Fatalf("block age advanced decoration RNG: next=%d expected=%d", actual, want)
			}
		})
	}
}
