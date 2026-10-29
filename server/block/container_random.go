package block

import "github.com/df-mc/dragonfly/server/item/inventory"

// randomContainerSlot follows the native dispenser/dropper reservoir walk.
// Every occupied slot consumes one bounded draw, including the first slot
// with bound one. Empty slots consume none.
func randomContainerSlot(inv *inventory.Inventory, word func() uint32) int {
	selected, count := -1, uint32(0)
	for slot := range inv.Size() {
		stack, _ := inv.Item(slot)
		if stack.Empty() {
			continue
		}
		count++
		if word()%count == 0 {
			selected = slot
		}
	}
	return selected
}
