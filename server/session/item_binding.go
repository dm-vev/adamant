package session

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

func ensureBindingAllowsRemoval(stack item.Stack, from, to protocol.StackRequestSlotInfo, c Controllable) error {
	if stack.Empty() || c.GameMode().CreativeInventory() || from.Container.ContainerID != protocol.ContainerArmor {
		return nil
	}
	if _, bound := stack.Enchantment(enchantment.CurseOfBinding); bound && (to.Container.ContainerID != protocol.ContainerArmor || from.Slot != to.Slot) {
		return fmt.Errorf("cursed item cannot be removed from armour slot")
	}
	return nil
}
