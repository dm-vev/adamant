package loot

import (
	"math"

	"github.com/df-mc/dragonfly/server/item"
)

// lootSetDamage ports SetItemDamageFunction: value is the fraction of
// durability to leave, added to what the item has left when add is set. It
// reports false for items without durability, which the function leaves
// alone.
func lootSetDamage(id string, components map[string]any, value float32, add bool) (int32, bool, error) {
	maxDamage, err := lootMaxDamage(id, components)
	if err != nil || maxDamage <= 0 {
		return 0, false, err
	}
	left := float32(0)
	if add {
		current, _ := components["minecraft:damage"].(int32)
		left = 1 - float32(current)/float32(maxDamage)
	}
	left = min(max(left+value, 0), 1)
	return int32(math.Floor(float64((1 - left) * float32(maxDamage)))), true, nil
}

// lootMaxDamage returns how much damage an item given by its Java id and
// components can take, which a max_damage component overrides.
func lootMaxDamage(id string, components map[string]any) (int32, error) {
	if n, ok := components["minecraft:max_damage"].(int32); ok {
		return n, nil
	}
	stack, err := item.StackFromJavaNBT(map[string]any{"id": id, "count": int32(1), "components": components})
	if err != nil {
		return 0, err
	}
	if durable, ok := stack.Item().(item.Durable); ok {
		return int32(durable.DurabilityInfo().MaxDurability), nil
	}
	return 0, nil
}
