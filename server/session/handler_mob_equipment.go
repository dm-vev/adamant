package session

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// MobEquipmentHandler handles the MobEquipment packet.
type MobEquipmentHandler struct{}

// Handle ...
func (*MobEquipmentHandler) Handle(p proto.Packet, s *Session, tx *world.Tx, c Controllable) error {
	pk := p.(*v2193.MobEquipmentPacket)

	if pk.EntityRuntimeID != selfEntityRuntimeID {
		return errSelfRuntimeID
	}
	switch pk.WindowID {
	case v2193.WindowIDOffHand:
		// This window ID is expected, but we don't handle it.
		return nil
	case v2193.WindowIDInventory:
		return s.VerifyAndSetHeldSlot(int(pk.InventorySlot), stackToItem(s.br, pk.NewItem.Stack), c)
	default:
		return fmt.Errorf("only main inventory should be involved in slot change, got window ID %v", pk.WindowID)
	}
}
