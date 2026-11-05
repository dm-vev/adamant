package item_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
)

// importedPotion imports a stack of the Java item id with the potion_contents
// value passed.
func importedPotion(t *testing.T, id string, contents any) item.Stack {
	t.Helper()
	s, err := item.StackFromStructureNBT(map[string]any{
		"id": id, "count": int32(1),
		"components": map[string]any{"minecraft:potion_contents": contents},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// A potion made in game has no components from an import, and exports with
// the type it has now: potions, splash and lingering potions and tipped
// arrows alike.
func TestJavaExportWritesThePotionOfAnItem(t *testing.T) {
	for _, c := range []struct {
		name string
		it   world.Item
		want string
	}{
		{"potion", item.Potion{Type: potion.StrongHealing()}, "minecraft:strong_healing"},
		{"water bottle", item.Potion{Type: potion.Water()}, "minecraft:water"},
		{"splash", item.SplashPotion{Type: potion.LongSwiftness()}, "minecraft:long_swiftness"},
		{"lingering", item.LingeringPotion{Type: potion.Poison()}, "minecraft:poison"},
		{"tipped arrow", item.Arrow{Tip: potion.StrongPoison()}, "minecraft:strong_poison"},
	} {
		contents, _ := exportedComponents(t, item.NewStack(c.it, 1))["minecraft:potion_contents"].(map[string]any)
		if contents["potion"] != c.want {
			t.Errorf("%s: potion_contents %v, want the potion %s", c.name, contents, c.want)
		}
	}
	if components := exportedComponents(t, item.NewStack(item.Arrow{}, 1)); components["minecraft:potion_contents"] != nil {
		t.Errorf("a plain arrow exports potion_contents %v", components["minecraft:potion_contents"])
	}
}

// An imported potion keeps the custom color, effects and name the server does
// not model, and takes the potion it has now: a potion changed in game does
// not export the one it came with.
func TestJavaExportKeepsWhatPotionContentsHoldBesideThePotion(t *testing.T) {
	s := importedPotion(t, "minecraft:potion", map[string]any{
		"potion": "minecraft:swiftness", "custom_color": int32(255), "custom_name": "x",
		"custom_effects": []any{map[string]any{"id": "minecraft:glowing", "duration": int32(100)}},
	})
	contents, _ := exportedComponents(t, s)["minecraft:potion_contents"].(map[string]any)
	if contents["potion"] != "minecraft:swiftness" || contents["custom_color"] == nil || contents["custom_name"] != "x" || contents["custom_effects"] == nil {
		t.Fatalf("imported contents exported as %v", contents)
	}
	changed := s.WithItem(item.Potion{Type: potion.Healing()})
	contents, _ = exportedComponents(t, changed)["minecraft:potion_contents"].(map[string]any)
	if contents["potion"] != "minecraft:healing" || contents["custom_color"] == nil || contents["custom_effects"] == nil {
		t.Errorf("changed potion exported as %v", contents)
	}
	// The retained component is not changed by the export.
	again, _ := exportedComponents(t, s)["minecraft:potion_contents"].(map[string]any)
	if again["potion"] != "minecraft:swiftness" {
		t.Errorf("a second export gives %v", again)
	}
}

// The short form of the component, the potion alone, exports as the compound.
func TestJavaExportWritesTheShortPotionContentsAsACompound(t *testing.T) {
	s := importedPotion(t, "minecraft:splash_potion", "minecraft:long_slow_falling")
	want := map[string]any{"potion": "minecraft:long_slow_falling"}
	if got := exportedComponents(t, s)["minecraft:potion_contents"]; !reflect.DeepEqual(got, want) {
		t.Errorf("potion_contents %v, want %v", got, want)
	}
}

// What the server cannot model is left as it came: a potion it has no type
// of, and a potion of custom effects only, which has no potion in Java.
func TestJavaExportLeavesPotionsTheServerHasNoTypeOf(t *testing.T) {
	for _, contents := range []map[string]any{
		{"potion": "mod:fancy"},
		{"custom_effects": []any{map[string]any{"id": "minecraft:glowing", "duration": int32(100)}}, "custom_color": int32(3)},
	} {
		got := exportedComponents(t, importedPotion(t, "minecraft:potion", contents))["minecraft:potion_contents"]
		// The retained components went through JSON: numbers are float64.
		a, _ := json.Marshal(got)
		b, _ := json.Marshal(contents)
		if string(a) != string(b) {
			t.Errorf("potion_contents %s, want %s as it came", a, b)
		}
	}
}
