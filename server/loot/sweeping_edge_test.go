package loot

import (
	"encoding/json"
	"testing"

	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
)

// The Sweeping Edge enchant_randomly and set_enchantments give a sword is
// the stack's own: the stack holds it, not only the Java components it was
// made of. Bedrock has no ID of it.
func TestLootGivesSweepingEdge(t *testing.T) {
	for _, raw := range []string{
		`{"type": "minecraft:enchant_randomly", "options": ["minecraft:sweeping_edge"]}`,
		`{"type": "minecraft:set_enchantments", "enchantments": {"minecraft:sweeping_edge": 3}}`,
	} {
		var m modifier
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatal(err)
		}
		random := vanilla.NewLegacyRandom(1)
		s, err := applyModifiers("minecraft:diamond_sword", 1, map[string]any{}, []modifier{m}, &Context{Random: &random})
		if err != nil {
			t.Fatal(err)
		}
		if e, ok := s.Enchantment(enchantment.SweepingEdge); !ok || e.Level() < 1 || e.Level() > 3 {
			t.Fatalf("%s: the sword holds %v", raw, s.Enchantments())
		}
		if _, ok := item.EnchantmentID(enchantment.SweepingEdge); ok {
			t.Fatal("Sweeping Edge has a Bedrock ID")
		}
	}
}
