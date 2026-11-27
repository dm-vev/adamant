package player

import (
	"testing"

	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/item/enchantment"
	"github.com/dm-vev/adamant/server/world"
)

func TestDamageBonusEntityCategories(t *testing.T) {
	t.Parallel()
	sword, ok := world.ItemByName("minecraft:diamond_sword", 0)
	if !ok {
		t.Fatal("diamond sword is unavailable")
	}
	smite := item.NewStack(sword, 1).WithEnchantments(item.NewEnchantment(enchantment.Smite, 3))
	if got := mobDamageBonus(smite, "minecraft:zombie"); got != 7.5 {
		t.Fatalf("Smite III bonus vs zombie = %v, want 7.5", got)
	}
	if got := mobDamageBonus(smite, "minecraft:spider"); got != 0 {
		t.Fatalf("Smite III bonus vs spider = %v, want 0", got)
	}
	bane := item.NewStack(sword, 1).WithEnchantments(item.NewEnchantment(enchantment.BaneOfArthropods, 2))
	if got := mobDamageBonus(bane, "minecraft:spider"); got != 5 {
		t.Fatalf("Bane II bonus vs spider = %v, want 5", got)
	}
	if got := mobDamageBonus(bane, "minecraft:zombie"); got != 0 {
		t.Fatalf("Bane II bonus vs zombie = %v, want 0", got)
	}
}
