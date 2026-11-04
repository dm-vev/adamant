package menu

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
)

// playerSlot reports whether s is a slot of the inventories of the player
// itself: the main inventory, the off hand, the armour or the cursor. An
// item locked in the inventory may move among them only.
func (t *Tx) playerSlot(s Slot) bool {
	inv := t.o.Inv
	switch {
	case s.Inv == nil:
		return false
	case s.Inv == inv.Main, s.Inv == inv.OffHand, t.armourSlot(s):
		return true
	}
	return s.Inv == inv.UI && s.Index == UICursor
}

// armourSlot reports whether s is a slot of the armour of the player.
func (t *Tx) armourSlot(s Slot) bool {
	return s.Inv != nil && t.o.Inv.Armour != nil && s.Inv == t.o.Inv.Armour.Inventory()
}

// sameSlot reports whether a and b are the same slot: of the same
// inventory, at the same index. The off hand has one slot only.
func (t *Tx) sameSlot(a, b Slot) bool {
	if a.Inv == nil || a.Inv != b.Inv {
		return false
	}
	if a.Inv == t.o.Inv.OffHand {
		return true
	}
	return a.Index == b.Index
}

// ensureUnlockedForInventoryMove refuses to move a locked stack out of the
// inventories of the player, or out of its slot, as its lock mode says.
func (t *Tx) ensureUnlockedForInventoryMove(stack item.Stack, from, to Slot) error {
	if stack.Empty() || !t.playerSlot(from) {
		return nil
	}
	switch stack.LockMode() {
	case item.LockInInventory:
		if !t.playerSlot(to) {
			return fmt.Errorf("item is locked in inventory")
		}
	case item.LockInSlot:
		if !t.sameSlot(from, to) {
			return fmt.Errorf("item is locked in slot")
		}
	}
	return nil
}

// ensureUnlockedForInventoryRemoval refuses to remove a locked stack from
// the inventories of the player.
func (t *Tx) ensureUnlockedForInventoryRemoval(stack item.Stack, from Slot) error {
	if stack.Empty() || !t.playerSlot(from) || !stack.Locked() {
		return nil
	}
	switch stack.LockMode() {
	case item.LockInInventory:
		return fmt.Errorf("item is locked in inventory")
	case item.LockInSlot:
		return fmt.Errorf("item is locked in slot")
	default:
		return nil
	}
}

// ensureUnlockedForCrafting refuses a locked stack as an input of a craft.
func ensureUnlockedForCrafting(stack item.Stack) error {
	if !stack.Locked() {
		return nil
	}
	return fmt.Errorf("item is locked and cannot be used as a crafting ingredient")
}

// ensureBindingAllowsRemoval refuses to take a stack cursed with binding
// out of an armour slot, but to the same slot, outside a game mode with the
// creative inventory. A to of no inventory is out of the inventories, as a
// drop.
func (t *Tx) ensureBindingAllowsRemoval(stack item.Stack, from, to Slot) error {
	if stack.Empty() || t.o.Owner.GameMode().CreativeInventory() || !t.armourSlot(from) {
		return nil
	}
	if _, bound := stack.Enchantment(enchantment.CurseOfBinding); bound && (!t.armourSlot(to) || from.Index != to.Index) {
		return fmt.Errorf("cursed item cannot be removed from armour slot")
	}
	return nil
}
