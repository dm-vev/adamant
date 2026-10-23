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

func TestProcessorSettingsRandomMatchesReference(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_settings_random.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Xoroshiro, Explicit bool
		Kind                string
		Seed, Next          int64
		Blocks              []struct {
			Position      [3]int32
			Input, Output json.RawMessage
		}
	}
	if err = json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		t.Run(fmt.Sprintf("%s/xoro=%v/explicit=%v", row.Kind, row.Xoroshiro, row.Explicit), func(t *testing.T) {
			random := vanilla.NewLegacyRandom(row.Seed)
			if row.Xoroshiro {
				random = vanilla.NewXoroshiroRandom(row.Seed)
			}
			env := ProcessorEnvironment{Random: &random}
			if row.Explicit {
				env.SettingsRandom = &random
			}
			config := map[string]any{"mossiness": float32(.5)}
			if row.Kind == "block_rot" {
				config = map[string]any{"integrity": float32(.5), "rottable_blocks": "#minecraft:ancient_city_replaceable"}
			}
			processors, err := LoadProcessors(nil, []ProcessorSpec{{Type: row.Kind, Config: config}}, env)
			if err != nil {
				t.Fatal(err)
			}
			var input, want []Block
			for _, b := range row.Blocks {
				input = append(input, Block{Pos: b.Position, State: oracleRuleState(t, b.Input)})
				if len(b.Output) > 0 && string(b.Output) != "null" {
					want = append(want, Block{Pos: b.Position, State: oracleRuleState(t, b.Output)})
				}
			}
			template := Template{Palettes: [][]Block{input}}
			placed, err := template.PlaceBlocks(0, [3]int32{}, None, NoMirror, Bounds{Min: [3]int32{-2000, -64, -1000}, Max: [3]int32{2000, 200, 1000}}, 0, processors...)
			if err != nil {
				t.Fatal(err)
			}
			if len(placed.Blocks) != len(want) {
				t.Fatalf("placed=%v source=%v", placed.Blocks, want)
			}
			for i, actual := range placed.Blocks {
				expected := want[i]
				if actual.Pos != expected.Pos || actual.State.Name != expected.State.Name || !maps.Equal(gamedata.BlockPropertiesWithDefaults(actual.State.Name, actual.State.Properties), expected.State.Properties) {
					t.Fatalf("block=%d actual=%+v source=%+v", i, actual, expected)
				}
			}
			if actual := random.NextLong(); actual != row.Next {
				t.Fatalf("following supplied RNG=%d source=%d", actual, row.Next)
			}
		})
	}
}
