package loot

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
)

func TestLootingEnchantedCountIncreaseBound(t *testing.T) {
	sword, ok := world.ItemByName("minecraft:diamond_sword", 0)
	if !ok {
		t.Fatal("diamond sword is unavailable")
	}
	plain := item.NewStack(sword, 1)
	looting := plain.WithEnchantments(item.NewEnchantment(enchantment.Looting, 3))
	increased := false
	for seed := int64(1); seed <= 64; seed++ {
		base, err := Generate("minecraft:entities/spider", seed, Context{PlayerKilled: true, ToolStack: plain})
		if err != nil {
			t.Fatal(err)
		}
		got, err := Generate("minecraft:entities/spider", seed, Context{PlayerKilled: true, ToolStack: looting})
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, stack := range got {
			if name, _ := stack.Item().(interface{ EncodeItem() (string, int16) }).EncodeItem(); name == "minecraft:string" {
				count += stack.Count()
				if stack.Count() > 5 {
					t.Fatalf("Looting III produced %d string, above spider maximum 5", stack.Count())
				}
			}
		}
		baseCount := 0
		for _, stack := range base {
			if name, _ := stack.Item().(interface{ EncodeItem() (string, int16) }).EncodeItem(); name == "minecraft:string" {
				baseCount += stack.Count()
			}
		}
		increased = increased || count > baseCount
	}
	if !increased {
		t.Fatal("Looting III never increased spider string drops over 64 deterministic seeds")
	}
}
