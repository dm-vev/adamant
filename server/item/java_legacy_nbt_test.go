package item_test

import (
	"regexp"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/potion"
)

// javaComponentID is an Identifier, which ItemStack.CODEC requires of the
// IDs of components: Damage or display is none.
var javaComponentID = regexp.MustCompile(`^[a-z0-9_.-]+:[a-z0-9_./-]+$`)

// The tag of a stack from before 1.20.5 becomes components as Java's
// ItemStackComponentizationFix makes them, in the forms 26.3 reads, and
// what it does not move goes into minecraft:custom_data: no legacy key is
// written back as a component.
func TestJavaLegacyTagBecomesComponents(t *testing.T) {
	s, err := item.StackFromStructureNBT(map[string]any{
		"id": "minecraft:iron_sword", "Count": int8(1),
		"tag": map[string]any{
			"Damage": int32(10), "RepairCost": int32(2), "Unbreakable": int8(1), "HideFlags": int32(1),
			"Enchantments": []any{
				map[string]any{"id": "minecraft:sharpness", "lvl": int16(2)},
				map[string]any{"id": "minecraft:smite", "lvl": int16(0)},
			},
			"display": map[string]any{
				"Name":  `{"text":"Blade"}`,
				"Lore":  []any{`"line one"`, `not json`},
				"color": int32(0xff0000),
				"Foo":   int8(1),
			},
			"CustomModelData": int32(7),
			"Trim":            map[string]any{"material": "minecraft:gold", "pattern": "minecraft:coast"},
			"EntityTag":       map[string]any{"id": "minecraft:pig"},
			"SomeKey":         "x",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.Durability() != 240 || s.AnvilCost() != 2 || !s.Unbreakable() {
		t.Errorf("durability %d, cost %d, unbreakable %v", s.Durability(), s.AnvilCost(), s.Unbreakable())
	}
	if e, ok := s.Enchantment(enchantment.Sharpness); !ok || e.Level() != 2 || len(s.Enchantments()) != 1 {
		t.Errorf("enchantments %v, want sharpness 2", s.Enchantments())
	}
	if s.CustomName() != "Blade" || !slices.Equal(s.Lore(), []string{"line one"}) {
		t.Errorf("named %q with lore %q, want Blade and the line of strict JSON", s.CustomName(), s.Lore())
	}
	data, err := item.StackToStructureNBT(s)
	if err != nil {
		t.Fatal(err)
	}
	components := data["components"].(map[string]any)
	for k := range components {
		if !javaComponentID.MatchString(k) {
			t.Errorf("component %q written back", k)
		}
	}
	if components["minecraft:damage"] != int32(10) || components["minecraft:repair_cost"] != int32(2) {
		t.Errorf("damage %v, repair cost %v", components["minecraft:damage"], components["minecraft:repair_cost"])
	}
	if levels, _ := components["minecraft:enchantments"].(map[string]any); len(levels) != 1 || levels["minecraft:sharpness"] != int32(2) {
		t.Errorf("enchantments %v", components["minecraft:enchantments"])
	}
	if floats, _ := components["minecraft:custom_model_data"].(map[string]any)["floats"].([]any); len(floats) != 1 || floats[0] != float64(7) {
		t.Errorf("custom model data %v, want {floats: [7]}", components["minecraft:custom_model_data"])
	}
	if components["minecraft:dyed_color"] != float64(0xff0000) {
		t.Errorf("dyed colour %v", components["minecraft:dyed_color"])
	}
	if trim, _ := components["minecraft:trim"].(map[string]any); trim["material"] != "minecraft:gold" || trim["pattern"] != "minecraft:coast" {
		t.Errorf("trim %v", components["minecraft:trim"])
	}
	custom, _ := components["minecraft:custom_data"].(map[string]any)
	display, _ := custom["display"].(map[string]any)
	if len(custom) != 3 || custom["SomeKey"] != "x" || custom["EntityTag"] == nil || len(display) != 1 || display["Foo"] != float64(1) {
		t.Errorf("custom data %v, want the rest of display, EntityTag and SomeKey", custom)
	}
}

// Legacy enchantments that leave no level give a glint (fixEnchantments);
// StoredEnchantments move only on an enchanted book; the potion of a potion
// moves into minecraft:potion_contents.
func TestJavaLegacyTagOfBooksAndPotions(t *testing.T) {
	book, err := item.StackFromStructureNBT(map[string]any{
		"id": "minecraft:enchanted_book", "Count": int8(1),
		"tag": map[string]any{"StoredEnchantments": []any{map[string]any{"id": "minecraft:mending", "lvl": int16(0)}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := item.StackToStructureNBT(book)
	if c := data["components"].(map[string]any); c["minecraft:enchantment_glint_override"] == nil || c["StoredEnchantments"] != nil {
		t.Errorf("book components %v, want a glint", c)
	}
	sword, err := item.StackFromStructureNBT(map[string]any{
		"id": "minecraft:iron_sword", "Count": int8(1),
		"tag": map[string]any{"StoredEnchantments": []any{map[string]any{"id": "minecraft:mending", "lvl": int16(1)}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sword.Enchantment(enchantment.Mending); ok {
		t.Error("a sword took StoredEnchantments as its enchantments")
	}
	data, _ = item.StackToStructureNBT(sword)
	if custom, _ := data["components"].(map[string]any)["minecraft:custom_data"].(map[string]any); custom["StoredEnchantments"] == nil {
		t.Errorf("sword components %v, want StoredEnchantments in custom data", data["components"])
	}
	p, err := item.StackFromStructureNBT(map[string]any{
		"id": "minecraft:potion", "Count": int8(1),
		"tag": map[string]any{"Potion": "minecraft:swiftness", "CustomPotionColor": int32(5)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := p.Item().(item.Potion); !ok || got.Type != potion.Swiftness() {
		t.Errorf("potion %v, want swiftness", p.Item())
	}
	data, _ = item.StackToStructureNBT(p)
	if contents, _ := data["components"].(map[string]any)["minecraft:potion_contents"].(map[string]any); contents["potion"] != "minecraft:swiftness" || contents["custom_color"] != float64(5) {
		t.Errorf("potion contents %v", data["components"])
	}
}
