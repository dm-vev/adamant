package loot

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/item"
)

type enchantmentFixture struct {
	ID    string `json:"id"`
	Level int    `json:"level"`
}

type enchantWithLevelsFixture struct {
	Select []struct {
		Item              string               `json:"item"`
		Cost              int                  `json:"cost"`
		Seed              string               `json:"seed"`
		Options           string               `json:"options"`
		ResultItem        string               `json:"result_item"`
		Empty             bool                 `json:"empty"`
		InputEnchantments string               `json:"input_enchantments"`
		InputDamage       int                  `json:"input_damage"`
		InputName         string               `json:"input_name"`
		Enchantments      []enchantmentFixture `json:"enchantments"`
		NextLong          string               `json:"next_long"`
	} `json:"select"`
	MobSpawnProvider []struct {
		Item         string               `json:"item"`
		Seed         string               `json:"seed"`
		Special      float32              `json:"special_multiplier"`
		Enchantments []enchantmentFixture `json:"enchantments"`
		NextLong     string               `json:"next_long"`
	} `json:"mob_spawn_provider"`
}

// Fixtures are captured by running Java 26.3 EnchantmentHelper and
// EnchantmentsByCostWithDifficulty against the real vanilla registries.
func TestEnchantWithLevelsJava263Source(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_enchant_with_levels.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures enchantWithLevelsFixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures.Select {
		t.Run(fixture.Item+"/"+fixture.Options+"/"+fixture.Seed, func(t *testing.T) {
			seed, err := strconv.ParseInt(fixture.Seed, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			var options []string
			if fixture.Options != "all" && fixture.Options != "empty" {
				if strings.HasPrefix(fixture.Options, "#") {
					options = append([]string{}, orderedEnchantmentTags[normalize(fixture.Options[1:])]...)
				} else {
					options = strings.Split(fixture.Options, ",")
				}
			}
			var base item.Stack
			if !fixture.Empty {
				inputComponents := map[string]any{}
				if fixture.InputEnchantments != "" {
					separator := strings.LastIndexByte(fixture.InputEnchantments, ':')
					if separator < 0 {
						t.Fatalf("invalid fixture input enchantment %q", fixture.InputEnchantments)
					}
					inputComponents["minecraft:enchantments"] = map[string]any{fixture.InputEnchantments[:separator]: int32(mustFixtureInt(t, fixture.InputEnchantments[separator+1:]))}
				}
				if fixture.InputDamage != 0 {
					inputComponents["minecraft:damage"] = int32(fixture.InputDamage)
				}
				if fixture.InputName != "" {
					inputComponents["minecraft:custom_name"] = map[string]any{"text": fixture.InputName}
				}
				baseData := map[string]any{"id": fixture.Item, "count": int32(1)}
				if len(inputComponents) > 0 {
					baseData["components"] = inputComponents
				}
				var err error
				base, err = item.StackFromJavaNBT(baseData)
				if err != nil {
					t.Fatal(err)
				}
			}
			random := java26.NewLegacyRandom(seed)
			got, err := enchantWithLevels(base, fixture.Cost, options, &random)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := item.StackToJavaNBT(got)
			if err != nil {
				t.Fatal(err)
			}
			if fixture.Empty {
				if !got.Empty() || encoded != nil {
					t.Fatalf("empty source stack became %#v / %#v", got, encoded)
				}
			} else {
				if encoded["id"] != fixture.ResultItem {
					t.Fatalf("item ID = %v, want %s", encoded["id"], fixture.ResultItem)
				}
				outputComponents, _ := encoded["components"].(map[string]any)
				component := "minecraft:enchantments"
				if fixture.ResultItem == "minecraft:enchanted_book" {
					component = "minecraft:stored_enchantments"
				}
				levels, _ := outputComponents[component].(map[string]any)
				if gotLevels := canonicalFixtureLevels(levels); !reflect.DeepEqual(gotLevels, sortedFixtureLevels(fixture.Enchantments)) {
					t.Fatalf("%s = %#v, want %#v", component, gotLevels, sortedFixtureLevels(fixture.Enchantments))
				}
				if fixture.InputDamage != 0 && outputComponents["minecraft:damage"] != int32(fixture.InputDamage) {
					t.Fatalf("damage = %v, want %d", outputComponents["minecraft:damage"], fixture.InputDamage)
				}
				if fixture.InputName != "" {
					name, _ := outputComponents["minecraft:custom_name"].(map[string]any)
					if name["text"] != fixture.InputName {
						t.Fatalf("custom name = %#v, want %q", name, fixture.InputName)
					}
				}
			}
			if next := strconv.FormatInt(random.NextLong(), 10); next != fixture.NextLong {
				t.Fatalf("nextLong = %s, want %s", next, fixture.NextLong)
			}
		})
	}
	for _, fixture := range fixtures.MobSpawnProvider {
		t.Run("mob-provider/"+fixture.Item+"/"+fixture.Seed, func(t *testing.T) {
			seed, err := strconv.ParseInt(fixture.Seed, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			base, err := item.StackFromJavaNBT(map[string]any{"id": fixture.Item, "count": int32(1)})
			if err != nil {
				t.Fatal(err)
			}
			random := java26.NewLegacyRandom(seed)
			got, err := EnchantMobSpawnEquipment(base, fixture.Special, &random)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := item.StackToJavaNBT(got)
			if err != nil {
				t.Fatal(err)
			}
			components, _ := encoded["components"].(map[string]any)
			levels, _ := components["minecraft:enchantments"].(map[string]any)
			if got := canonicalFixtureLevels(levels); !reflect.DeepEqual(got, sortedFixtureLevels(fixture.Enchantments)) {
				t.Fatalf("enchantments = %#v, want %#v", got, sortedFixtureLevels(fixture.Enchantments))
			}
			if next := strconv.FormatInt(random.NextLong(), 10); next != fixture.NextLong {
				t.Fatalf("nextLong = %s, want %s", next, fixture.NextLong)
			}
		})
	}
}

func canonicalFixtureLevels(levels map[string]any) []enchantmentFixture {
	var out []enchantmentFixture
	for id, raw := range levels {
		var level int
		switch v := raw.(type) {
		case int32:
			level = int(v)
		case int:
			level = v
		case float64:
			level = int(v)
		}
		out = append(out, enchantmentFixture{ID: id, Level: level})
	}
	return sortedFixtureLevels(out)
}

func sortedFixtureLevels(levels []enchantmentFixture) []enchantmentFixture {
	out := append([]enchantmentFixture(nil), levels...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func mustFixtureInt(t *testing.T, value string) int {
	t.Helper()
	n, err := strconv.Atoi(value)
	if err != nil {
		t.Fatalf("invalid fixture integer %q: %v", value, err)
	}
	return n
}
