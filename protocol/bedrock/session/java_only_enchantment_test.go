package session

import (
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/menu"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// The enchanting table sends a client the enchantments of an option it has
// IDs of. Sweeping Edge is left out: an option of it alone is sent with its
// cost and no enchantment.
func TestViewEnchantmentOptionsLeavesOutJavaOnly(t *testing.T) {
	s := testSession("enchanter")
	s.ViewEnchantmentOptions([3]menu.EnchantmentOption{
		{Cost: 3, Enchantments: []item.Enchantment{item.NewEnchantment(enchantment.Sharpness, 1), item.NewEnchantment(enchantment.SweepingEdge, 1)}},
		{Cost: 9, Enchantments: []item.Enchantment{item.NewEnchantment(enchantment.SweepingEdge, 2)}},
		{Cost: 20, Enchantments: []item.Enchantment{item.NewEnchantment(enchantment.SweepingEdge, 3), item.NewEnchantment(enchantment.Unbreaking, 2)}},
	})
	pk, ok := s.testPacket().(*mc.PlayerEnchantOptionsPacket)
	if !ok {
		t.Fatal("no PlayerEnchantOptions")
	}
	want := [][]mc.EnchantmentInstance{{{Type: 9, Level: 1}}, {}, {{Type: 17, Level: 2}}}
	if len(pk.Options) != 3 {
		t.Fatalf("%d options", len(pk.Options))
	}
	for i, o := range pk.Options {
		if o.Cost != []uint8{3, 9, 20}[i] || o.RecipeNetworkID != uint32(i) || o.Enchantments.Slot != int32(i) {
			t.Errorf("option %d: cost %d, recipe %d, slot %d", i, o.Cost, o.RecipeNetworkID, o.Enchantments.Slot)
		}
		if got := o.Enchantments.Enchantments[1]; !reflect.DeepEqual(got, want[i]) {
			t.Errorf("option %d shows %v, want %v", i, got, want[i])
		}
	}
}
