package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
)

func TestJava26DamageBonusEntityCategories(t *testing.T) {
	sword, ok := world.ItemByName("minecraft:diamond_sword", 0)
	if !ok {
		t.Fatal("diamond sword is unavailable")
	}
	smite := item.NewStack(sword, 1).WithEnchantments(item.NewEnchantment(enchantment.Smite, 3))
	if got := java26MobDamageBonus(smite, "minecraft:zombie"); got != 7.5 {
		t.Fatalf("Smite III bonus vs zombie = %v, want 7.5", got)
	}
	if got := java26MobDamageBonus(smite, "minecraft:spider"); got != 0 {
		t.Fatalf("Smite III bonus vs spider = %v, want 0", got)
	}
	bane := item.NewStack(sword, 1).WithEnchantments(item.NewEnchantment(enchantment.BaneOfArthropods, 2))
	if got := java26MobDamageBonus(bane, "minecraft:spider"); got != 5 {
		t.Fatalf("Bane II bonus vs spider = %v, want 5", got)
	}
	if got := java26MobDamageBonus(bane, "minecraft:zombie"); got != 0 {
		t.Fatalf("Bane II bonus vs zombie = %v, want 0", got)
	}
}
