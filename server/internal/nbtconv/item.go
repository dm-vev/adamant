package nbtconv

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
)

// InvFromNBT decodes the data of an NBT slice into the inventory passed. The
// optional context is the parent NBT map supplied to world.DecodeNBT.
func InvFromNBT(inv *inventory.Inventory, items []any, context ...map[string]any) {
	var registry world.BlockRegistry = world.DefaultBlockRegistry
	if len(context) != 0 {
		registry = world.BlockRegistryFromNBT(context[0])
	}
	for _, itemData := range items {
		data, _ := itemData.(map[string]any)
		it := item.ReadNBT(data, nil, registry)
		if it.Empty() {
			continue
		}
		_ = inv.SetItem(int(Uint8(data, "Slot")), it)
	}
}

// InvToNBT encodes an inventory to a data slice which may be encoded as NBT.
func InvToNBT(inv *inventory.Inventory) []map[string]any {
	var items []map[string]any
	for index, i := range inv.Slots() {
		if i.Empty() {
			continue
		}
		data := item.WriteNBT(i, true)
		data["Slot"] = byte(index)
		items = append(items, data)
	}
	return items
}
