package loot

import (
	"encoding/json"
	"os"
	"reflect"
	"strconv"
	"testing"

	"github.com/df-mc/dragonfly/server/internal/vanilla"
)

// Fixtures execute Java 26.3 EnchantRandomlyFunction.run with real registries.
// Complete components and the next draw catch book replacement, holder order,
// compatibility filtering, level upgrades and degenerate random bounds.
func TestEnchantRandomlyJava263Source(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_enchant_randomly.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Input struct {
			Name       string         `json:"name"`
			Item       string         `json:"item"`
			Count      int            `json:"count"`
			Components map[string]any `json:"components"`
			Function   map[string]any `json:"function"`
		} `json:"input"`
		Seed     string         `json:"seed"`
		Result   map[string]any `json:"result"`
		NextLong string         `json:"next_long"`
	}
	if err = json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		t.Run(row.Input.Name+"/"+row.Seed, func(t *testing.T) {
			row.Input.Function["type"] = "minecraft:enchant_randomly"
			functionJSON, err := json.Marshal(row.Input.Function)
			if err != nil {
				t.Fatal(err)
			}
			var function modifier
			if err = json.Unmarshal(functionJSON, &function); err != nil {
				t.Fatal(err)
			}
			seed, err := strconv.ParseInt(row.Seed, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			count := row.Input.Count
			if count == 0 {
				count = 1
			}
			components := row.Input.Components
			if components == nil {
				components = make(map[string]any)
			}
			random := vanilla.NewLegacyRandom(seed)
			id, count, components, err := applyRandomEnchantment(row.Input.Item, count, components, function, &Context{Random: &random})
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]any{"id": id, "count": count}
			if len(components) != 0 {
				got["components"] = components
			}
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			var normalized map[string]any
			if err = json.Unmarshal(encoded, &normalized); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(normalized, row.Result) {
				t.Fatalf("Java stack = %#v, want %#v", normalized, row.Result)
			}
			if next := strconv.FormatInt(random.NextLong(), 10); next != row.NextLong {
				t.Fatalf("nextLong = %s, want %s", next, row.NextLong)
			}
		})
	}
}
