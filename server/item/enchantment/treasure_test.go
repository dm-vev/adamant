package enchantment_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
)

// The treasure enchantments of Java 26.3
// (data/minecraft/tags/enchantment/treasure.json), which an enchanting
// table never offers: #in_enchanting_table holds #non_treasure only.
var javaTreasure = map[string]bool{
	"minecraft:binding_curse": true, "minecraft:vanishing_curse": true, "minecraft:swift_sneak": true,
	"minecraft:soul_speed": true, "minecraft:frost_walker": true, "minecraft:mending": true,
	"minecraft:wind_burst": true,
}

func TestTreasureEnchantments(t *testing.T) {
	for _, e := range item.Enchantments() {
		tr, ok := e.(interface{ Treasure() bool })
		got := ok && tr.Treasure()
		if want := javaTreasure[item.JavaEnchantmentID(e)]; got != want {
			t.Errorf("%s: treasure %v, want %v", e.Name(), got, want)
		}
	}
}
