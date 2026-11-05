package item_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
)

func importedJavaSword(t *testing.T) item.Stack {
	t.Helper()
	s, err := item.StackFromStructureNBT(map[string]any{
		"id": "minecraft:diamond_sword", "count": int32(1),
		"components": map[string]any{
			"minecraft:damage":       int32(61),
			"minecraft:custom_name":  map[string]any{"text": "Blade"},
			"minecraft:lore":         []any{"plain"},
			"minecraft:repair_cost":  int32(7),
			"minecraft:enchantments": map[string]any{"minecraft:sharpness": int32(5), "minecraft:unbreaking": int32(3), "mod:modded": int32(2)},
			"minecraft:custom_data":  map[string]any{"owner": "x"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func exportedComponents(t *testing.T, s item.Stack) map[string]any {
	t.Helper()
	data, err := item.StackToStructureNBT(s)
	if err != nil {
		t.Fatal(err)
	}
	components, _ := data["components"].(map[string]any)
	return components
}

// An item that came from Java is written back as the server has it now. What
// a grindstone or an anvil took off it, an enchantment, the name, the lore,
// the damage and the repair cost, is not written again from the components it
// came with; only what the server does not model is kept, such as the
// enchantments it has no type of and the custom data.
func TestJavaExportWritesCurrentState(t *testing.T) {
	s := importedJavaSword(t).
		WithoutEnchantments(enchantment.Sharpness).
		WithCustomName().
		WithLore().
		WithAnvilCost(0).
		WithDurability(1561)
	components := exportedComponents(t, s)
	levels, _ := components["minecraft:enchantments"].(map[string]any)
	if _, stale := levels["minecraft:sharpness"]; stale {
		t.Errorf("a removed enchantment came back: %v", levels)
	}
	if level, _ := levels["minecraft:unbreaking"].(int32); level != 3 {
		t.Errorf("unbreaking written as %v, want 3", levels["minecraft:unbreaking"])
	}
	if level := levels["mod:modded"]; level == nil {
		t.Errorf("an enchantment the server has no type of was dropped: %v", levels)
	}
	for _, stale := range []string{"minecraft:damage", "minecraft:custom_name", "minecraft:lore", "minecraft:repair_cost"} {
		if v, ok := components[stale]; ok {
			t.Errorf("%s = %v came back after it was cleared", stale, v)
		}
	}
	if _, ok := components["minecraft:custom_data"]; !ok {
		t.Errorf("custom data was dropped: %v", components)
	}
}

// With every enchantment gone the component goes too, except for what the
// server cannot name.
func TestJavaExportDropsAnEmptyEnchantmentsComponent(t *testing.T) {
	plain, err := item.StackFromStructureNBT(map[string]any{
		"id": "minecraft:iron_sword", "components": map[string]any{
			"minecraft:enchantments": map[string]any{"minecraft:sharpness": int32(1)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := exportedComponents(t, plain.WithoutEnchantments(enchantment.Sharpness)); got["minecraft:enchantments"] != nil {
		t.Errorf("the enchantments of an unenchanted sword were written: %v", got)
	}
	book, err := item.StackFromStructureNBT(map[string]any{
		"id": "minecraft:enchanted_book", "components": map[string]any{
			"minecraft:stored_enchantments": map[string]any{"minecraft:mending": int32(1)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// A book that lost its last enchantment is a plain book again.
	if got := exportedComponents(t, book.WithoutEnchantments(enchantment.Mending)); got["minecraft:stored_enchantments"] != nil {
		t.Errorf("a book written with the enchantments it lost: %v", got)
	}
}
