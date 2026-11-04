package menu

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/item"
)

// ensureUnlockedForCrafting fails for a stack locked in the inventory or in
// its slot: a locked item is no ingredient of a craft.
func ensureUnlockedForCrafting(stack item.Stack) error {
	if !stack.Locked() {
		return nil
	}
	return fmt.Errorf("item is locked and cannot be used as a crafting ingredient")
}
