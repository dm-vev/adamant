package item_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
)

// Java 26 ItemStack.CODEC: {id, count, components}. The id defaults to the
// minecraft namespace and count to 1; damage, custom_name, lore,
// repair_cost, unbreakable, enchantments and stored_enchantments map onto
// the stack, and encoding writes them back under the same components.
func TestJavaStackComponents(t *testing.T) {
	s, err := item.StackFromStructureNBT(map[string]any{
		"id":    "diamond_sword",
		"count": int32(1),
		"components": map[string]any{
			"minecraft:damage":       int32(61),
			"minecraft:custom_name":  map[string]any{"text": "Blade"},
			"minecraft:lore":         []any{"plain", map[string]any{"text": "styled"}},
			"minecraft:repair_cost":  int32(3),
			"minecraft:unbreakable":  map[string]any{},
			"minecraft:enchantments": map[string]any{"minecraft:sharpness": int32(5), "minecraft:unbreaking": int32(3), "minecraft:unknown_mod": int32(1)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Item().(item.Sword); !ok || s.Count() != 1 {
		t.Fatalf("decoded %v", s)
	}
	if s.Durability() != 1500 || !s.Unbreakable() || s.CustomName() != "Blade" || s.AnvilCost() != 3 {
		t.Fatalf("durability %d, unbreakable %v, name %q, cost %d", s.Durability(), s.Unbreakable(), s.CustomName(), s.AnvilCost())
	}
	if lore := s.Lore(); len(lore) != 2 || lore[0] != "plain" || lore[1] != "styled" {
		t.Fatalf("lore %q", lore)
	}
	if e, ok := s.Enchantment(enchantment.Sharpness); !ok || e.Level() != 5 {
		t.Fatalf("sharpness %v", s.Enchantments())
	}
	if e, ok := s.Enchantment(enchantment.Unbreaking); !ok || e.Level() != 3 {
		t.Fatalf("unbreaking %v", s.Enchantments())
	}

	encoded, err := item.StackToStructureNBT(s)
	if err != nil {
		t.Fatal(err)
	}
	components := encoded["components"].(map[string]any)
	if encoded["id"] != "minecraft:diamond_sword" || encoded["count"] != int32(1) {
		t.Fatalf("encoded %v", encoded)
	}
	if components["minecraft:damage"] != int32(61) || components["minecraft:repair_cost"] != int32(3) {
		t.Fatalf("damage %v, repair cost %v", components["minecraft:damage"], components["minecraft:repair_cost"])
	}
	// Retained components pass through JSON, so their numbers may come back
	// as doubles, which NbtOps reads as numbers all the same.
	levels := components["minecraft:enchantments"].(map[string]any)
	for id, want := range map[string]float64{"minecraft:sharpness": 5, "minecraft:unbreaking": 3, "minecraft:unknown_mod": 1} {
		if got := number(levels[id]); got != want || len(levels) != 3 {
			t.Fatalf("enchantments %v; want %s at %v, and an unknown enchantment kept", levels, id, want)
		}
	}

	// Reading it back gives the same item; the raw components it retains
	// may differ in number types only.
	back, err := item.StackFromStructureNBT(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if back.Count() != s.Count() || back.Durability() != s.Durability() || back.CustomName() != s.CustomName() ||
		back.AnvilCost() != s.AnvilCost() || back.Unbreakable() != s.Unbreakable() || len(back.Enchantments()) != len(s.Enchantments()) {
		t.Fatalf("round trip = %v, want %v", back, s)
	}
}

func number(v any) float64 {
	switch n := v.(type) {
	case int32:
		return float64(n)
	case float64:
		return n
	}
	return -1
}

// An enchanted book holds its enchantments in stored_enchantments, which
// Java reads and writes apart from the enchantments of other items.
func TestJavaEnchantedBook(t *testing.T) {
	s, err := item.StackFromStructureNBT(map[string]any{
		"id":         "minecraft:enchanted_book",
		"components": map[string]any{"minecraft:stored_enchantments": map[string]any{"minecraft:mending": int32(1)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Enchantment(enchantment.Mending); !ok || s.Count() != 1 {
		t.Fatalf("book = %v", s)
	}
	encoded, err := item.StackToStructureNBT(s)
	if err != nil {
		t.Fatal(err)
	}
	components := encoded["components"].(map[string]any)
	if _, ok := components["minecraft:enchantments"]; ok {
		t.Fatal("a book wrote active enchantments")
	}
	if levels := components["minecraft:stored_enchantments"].(map[string]any); levels["minecraft:mending"] != int32(1) {
		t.Fatalf("stored enchantments %v", levels)
	}
}

// Enchantment levels are positive: ItemEnchantments.CODEC takes levels in
// 1..255, so zero and negative levels are dropped and larger ones capped.
func TestJavaEnchantmentLevels(t *testing.T) {
	s, err := item.StackFromStructureNBT(map[string]any{
		"id": "minecraft:iron_pickaxe",
		"components": map[string]any{"minecraft:enchantments": map[string]any{
			"minecraft:efficiency": int32(1000),
			"minecraft:fortune":    int32(0),
			"minecraft:unbreaking": int32(-1),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if e, ok := s.Enchantment(enchantment.Efficiency); !ok || e.Level() != 255 {
		t.Fatalf("efficiency %v", s.Enchantments())
	}
	if len(s.Enchantments()) != 1 {
		t.Fatalf("enchantments %v, want efficiency only", s.Enchantments())
	}
}

// ItemStack.getDamageValue clamps the damage component to max_damage, and
// ItemStack.CODEC keeps a stack whose damage is at or past it: the stack is
// not lost on load. Here it keeps the last point of durability, which
// survives a save in the Bedrock NBT, and its next use breaks it.
func TestJavaStackDamageAtMaximum(t *testing.T) {
	for _, damage := range []int32{1560, 1561, 5000} {
		s, err := item.StackFromStructureNBT(map[string]any{
			"id":         "minecraft:diamond_sword",
			"components": map[string]any{"minecraft:damage": damage},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := s.Item().(item.Sword); !ok || s.Durability() != 1 {
			t.Fatalf("damage %d: decoded %v with durability %d, want a sword with 1", damage, s.Item(), s.Durability())
		}
		if back := item.ReadNBT(item.WriteNBT(s)); back.Durability() != 1 {
			t.Fatalf("damage %d: %v after a Bedrock save, want durability 1", damage, back)
		}
		if broken := s.Damage(1); !broken.Empty() {
			t.Fatalf("damage %d: %v after one use, want the sword broken", damage, broken)
		}
	}
}

func TestJavaStackErrors(t *testing.T) {
	for name, data := range map[string]map[string]any{
		"no id":          {"count": int32(1)},
		"empty id":       {"id": ""},
		"unknown item":   {"id": "minecraft:not_an_item"},
		"negative count": {"id": "minecraft:stone", "count": int32(-1)},
	} {
		if _, err := item.StackFromStructureNBT(data); err == nil {
			t.Errorf("%s: decoded", name)
		}
	}
	if data, err := item.StackToStructureNBT(item.Stack{}); data != nil || err != nil {
		t.Errorf("empty stack encoded as %v, %v", data, err)
	}
}

// Before 1.20.5 a stack kept its data in "tag", with enchantments as a list
// of {id, lvl} compounds (DataFixers' ItemStackComponentizationFix reads
// Enchantments and StoredEnchantments this way).
func TestJavaLegacyStackTag(t *testing.T) {
	s, err := item.StackFromStructureNBT(map[string]any{
		"id":    "minecraft:iron_sword",
		"Count": int8(1),
		"tag": map[string]any{
			"Damage":       int32(10),
			"RepairCost":   int32(2),
			"Unbreakable":  int8(1),
			"Enchantments": []any{map[string]any{"id": "minecraft:sharpness", "lvl": int16(2)}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.Durability() != 240 || s.AnvilCost() != 2 || !s.Unbreakable() {
		t.Fatalf("durability %d, cost %d, unbreakable %v", s.Durability(), s.AnvilCost(), s.Unbreakable())
	}
	if e, ok := s.Enchantment(enchantment.Sharpness); !ok || e.Level() != 2 {
		t.Fatalf("legacy enchantments %v, want sharpness 2", s.Enchantments())
	}
	book, err := item.StackFromStructureNBT(map[string]any{
		"id":  "minecraft:enchanted_book",
		"tag": map[string]any{"StoredEnchantments": []any{map[string]any{"id": "minecraft:mending", "lvl": int16(1)}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := book.Enchantment(enchantment.Mending); !ok {
		t.Fatalf("legacy stored enchantments %v, want mending", book.Enchantments())
	}
}
