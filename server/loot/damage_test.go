package loot

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
)

// TestSetDamageLeavesFractionOfDurability checks set_damage as
// SetItemDamageFunction: the value is the fraction of durability left, not
// a damage count, and with add it adds to the fraction the item has left.
func TestSetDamageLeavesFractionOfDurability(t *testing.T) {
	add := true
	// damage ports the function's float maths for an item with max damage m.
	damage := func(m int, current int, value float32, add bool) int {
		left := float32(0)
		if add {
			left = 1 - float32(current)/float32(m)
		}
		left = min(max(left+value, 0), 1)
		return int(math.Floor(float64((1 - left) * float32(m))))
	}
	for _, test := range []struct {
		name string
		mods modifierList
		want func(m int) int
	}{
		{"quarter", modifierList{{Type: "minecraft:set_damage", Damage: 0.25}}, func(m int) int { return damage(m, 0, 0.25, false) }},
		{"added", modifierList{{Type: "minecraft:set_damage", Damage: 0.5}, {Type: "minecraft:set_damage", Damage: 0.25, Add: &add}},
			func(m int) int { return damage(m, damage(m, 0, 0.5, false), 0.25, true) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			stack, err := makeStack(entry{Type: "minecraft:item", Name: "minecraft:fishing_rod", Modifier: test.mods}, &Context{})
			if err != nil {
				t.Fatal(err)
			}
			maxDurability := stack.Item().(item.Durable).DurabilityInfo().MaxDurability
			if want := maxDurability - test.want(maxDurability); stack.Durability() != want {
				t.Fatalf("fishing rod durability = %d, want %d of %d", stack.Durability(), want, maxDurability)
			}
		})
	}
}

// TestSetCountAddsWhenAdding checks set_count as SetItemCountFunction: with
// add, the count is added to the count the item has.
func TestSetCountAddsWhenAdding(t *testing.T) {
	add := true
	stack, err := makeStack(entry{Type: "minecraft:item", Name: "minecraft:stick", Modifier: modifierList{
		{Type: "minecraft:set_count", Count: 3},
		{Type: "minecraft:set_count", Count: 2, Add: &add},
	}}, &Context{})
	if err != nil {
		t.Fatal(err)
	}
	if stack.Count() != 5 {
		t.Fatalf("3 sticks with 2 added = %d sticks, want 5", stack.Count())
	}
}
