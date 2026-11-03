package player

import (
	"fmt"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/event"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
)

// DropFromInventory drops count items of the stack in slot of the inventory
// of the player, as its client throws them out of the inventory, and removes
// from the slot as many as were dropped. It fails if the slot holds fewer
// than count items, if count is not positive, or if the stack is locked in
// the inventory or in its slot.
//
// The handler of the inventory sees the drop first, with the index of the
// held slot rather than slot, and may cancel it. The handler of the player
// then sees it as it sees every drop (Drop).
func (p *Player) DropFromInventory(slot, count int) error {
	actual, _ := p.inv.Item(slot)
	if count < 1 {
		return fmt.Errorf("expected at least one item to be dropped, got %d", count)
	}
	if count > actual.Count() {
		return fmt.Errorf("tried to throw %v items, but held only %v in slot", count, actual.Count())
	}
	if actual.Locked() {
		switch actual.LockMode() {
		case item.LockInInventory:
			return fmt.Errorf("item is locked in inventory")
		case item.LockInSlot:
			return fmt.Errorf("item is locked in slot")
		}
	}

	// Only grow or shrink the stack in the slot, so that nothing of the
	// stack the client claimed to throw is used.
	res := actual.Grow(count - actual.Count())
	ctx := event.C(inventory.Holder(p))
	if p.inv.Handler().HandleDrop(ctx, int(atomic.LoadUint32(p.heldSlot)), res); ctx.Cancelled() {
		return fmt.Errorf("action was cancelled")
	}
	n := p.Drop(res)
	_ = p.inv.SetItem(slot, actual.Grow(-n))
	return nil
}
