package session

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

func ensureBindingAllowsRemoval(stack item.Stack, from, to v2193.StackRequestSlotInfo, c Controllable) error {
	if stack.Empty() || c.GameMode().CreativeInventory() || from.Container.ContainerID != v2193.ContainerArmor {
		return nil
	}
	if _, bound := stack.Enchantment(enchantment.CurseOfBinding); bound && (to.Container.ContainerID != v2193.ContainerArmor || from.Slot != to.Slot) {
		return fmt.Errorf("cursed item cannot be removed from armour slot")
	}
	return nil
}
