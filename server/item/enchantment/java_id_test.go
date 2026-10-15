package enchantment

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
)

// TestJavaEnchantmentsSurviveConversion checks enchantments keep their Java
// registry IDs both ways, including Luck of the Sea, which Bedrock numbers
// 23, and the curses, which Java names binding_curse and vanishing_curse.
func TestJavaEnchantmentsSurviveConversion(t *testing.T) {
	for id, e := range map[string]item.EnchantmentType{
		"minecraft:luck_of_the_sea": LuckOfTheSea,
		"minecraft:binding_curse":   CurseOfBinding,
		"minecraft:vanishing_curse": CurseOfVanishing,
		"minecraft:sharpness":       Sharpness,
	} {
		if got, ok := item.EnchantmentByID(enchantmentNumericID(t, e)); !ok || got != e {
			t.Fatalf("%s is not registered under its Bedrock ID", id)
		}
		stack, err := item.StackFromJavaNBT(map[string]any{
			"id": "minecraft:enchanted_book", "count": int32(1),
			"components": map[string]any{"minecraft:stored_enchantments": map[string]any{id: int32(1)}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := stack.Enchantment(e); !ok {
			t.Fatalf("book with %s lost the enchantment: %v", id, stack.Enchantments())
		}
		data, err := item.StackToJavaNBT(stack)
		if err != nil {
			t.Fatal(err)
		}
		levels := data["components"].(map[string]any)["minecraft:stored_enchantments"].(map[string]any)
		if levels[id] != int32(1) {
			t.Fatalf("book with %s saved its enchantments as %v", id, levels)
		}
	}
}

func enchantmentNumericID(t *testing.T, e item.EnchantmentType) int {
	id, ok := item.EnchantmentID(e)
	if !ok {
		t.Fatalf("%s has no Bedrock ID", e.Name())
	}
	return id
}
