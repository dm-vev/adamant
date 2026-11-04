package menu

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/event"
	"github.com/df-mc/dragonfly/server/item/inventory"
)

// Drop drops count items of a slot in front of the player, as Player.Drop
// does, after the HandleDrop handler of the inventory. The slot keeps what
// the player did not drop, which the handlers of the player may cancel. It
// refuses a stack cursed with binding in an armour slot and a locked item,
// as Move does.
func (t *Tx) Drop(from Ref, count int) error {
	i, _ := t.Item(from.Slot)
	if err := t.ensureBindingAllowsRemoval(i, from.Slot, Slot{}); err != nil {
		return err
	}
	if err := t.ensureUnlockedForInventoryRemoval(i, from.Slot); err != nil {
		return err
	}
	if i.Count() < count {
		return fmt.Errorf("client attempted to drop %v items, but only %v present", count, i.Count())
	}

	if err := call(event.C(inventory.Holder(t.o.Owner)), from.Index, i.Grow(count-i.Count()), from.Inv.Handler().HandleDrop); err != nil {
		return err
	}

	n := t.o.Owner.Drop(i.Grow(count - i.Count()))
	if err := t.set(from, i.Grow(-n)); err != nil {
		return err
	}
	return nil
}

// Destroy removes count items of a slot. Only a player in a game mode with
// the creative inventory may, and not a locked item.
func (t *Tx) Destroy(from Ref, count int) error {
	if !t.o.Owner.GameMode().CreativeInventory() {
		return fmt.Errorf("can only destroy items in gamemode creative/spectator")
	}
	i, _ := t.Item(from.Slot)
	if err := t.ensureUnlockedForInventoryRemoval(i, from.Slot); err != nil {
		return err
	}
	if i.Count() < count {
		return fmt.Errorf("client attempted to destroy %v items, but only %v present", count, i.Count())
	}

	if err := t.set(from, i.Grow(-count)); err != nil {
		return err
	}
	return nil
}

// Touch sets a slot to what it holds, so that it counts as changed: a tool
// that wore while mining.
func (t *Tx) Touch(r Ref) error {
	i, _ := t.Item(r.Slot)
	if err := t.set(r, i); err != nil {
		return err
	}
	return nil
}
