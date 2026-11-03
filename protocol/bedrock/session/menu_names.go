package session

import (
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// slotName returns the name of the slot of a container of item stack
// requests: the container ID and the slot as the client names them.
func slotName(container, slot byte) menu.Name {
	return menu.Name(container)<<8 | menu.Name(slot)
}

// nameParts returns the container ID and the slot of a name.
func nameParts(n menu.Name) (container, slot byte) {
	return byte(n >> 8), byte(n)
}

// roleContainers are the containers the session names the slots a menu
// changes of its own accord by, as the session wrote those slots: the slot
// is the index of the slot in its inventory. Both inputs of a trade are
// named as the first, as handleMerchantCraft wrote them.
var roleContainers = map[menu.Role]byte{
	menu.RoleInventory:             v2193.ContainerCombinedHotBarAndInventory,
	menu.RoleCraftingGrid:          v2193.ContainerCraftingInput,
	menu.RoleCartographyInput:      v2193.ContainerCartographyInput,
	menu.RoleCartographyAdditional: v2193.ContainerCartographyAdditional,
	menu.RoleResult:                v2193.ContainerCreatedOutput,
	menu.RoleEnchantingInput:       v2193.ContainerEnchantingInput,
	menu.RoleEnchantingLapis:       v2193.ContainerEnchantingMaterial,
	menu.RoleAnvilInput:            v2193.ContainerAnvilInput,
	menu.RoleAnvilMaterial:         v2193.ContainerAnvilMaterial,
	menu.RoleGrindstoneFirst:       v2193.ContainerGrindstoneInput,
	menu.RoleGrindstoneSecond:      v2193.ContainerGrindstoneAdditional,
	menu.RoleSmithingInput:         v2193.ContainerSmithingTableInput,
	menu.RoleSmithingMaterial:      v2193.ContainerSmithingTableMaterial,
	menu.RoleSmithingTemplate:      v2193.ContainerSmithingTableTemplate,
	menu.RoleLoomBanner:            v2193.ContainerLoomInput,
	menu.RoleLoomDye:               v2193.ContainerLoomDye,
	menu.RoleStonecutterInput:      v2193.ContainerStonecutterInput,
	menu.RoleBeaconPayment:         v2193.ContainerBeaconPayment,
	menu.RoleTradeInput:            v2193.ContainerTradeTwoIngredientOne,
}

// nameSlot is the menu.Namer of the session.
func (s *Session) nameSlot(sl menu.Slot, r menu.Role) menu.Name {
	return slotName(roleContainers[r], byte(sl.Index))
}

// ref returns the slot a slot of an item stack request names, with the
// name the client gave it: the inventory as invByID finds it, the off hand
// at index 0 whatever slot the client names it by. It reports false for a
// container that is not there.
func (s *Session) ref(slot v2193.StackRequestSlotInfo, tx *world.Tx) (menu.Ref, bool) {
	inv, ok := s.invByID(int32(slot.Container.ContainerID), tx)
	if !ok {
		return menu.Ref{}, false
	}
	index := int(slot.Slot)
	if inv == s.offHand {
		index = 0
	}
	return menu.Ref{Slot: menu.Slot{Inv: inv, Index: index}, Name: slotName(slot.Container.ContainerID, slot.Slot)}, true
}
