package session

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// InteractHandler handles the Interact packet.
type InteractHandler struct{}

// Handle ...
func (h *InteractHandler) Handle(p proto.Packet, s *Session, tx *world.Tx, c Controllable) error {
	pk := p.(*mc.InteractPacket)

	switch pk.ActionType {
	case mc.InteractActionLeaveVehicle:
		if pk.TargetEntityRuntimeID == selfEntityRuntimeID {
			return errSelfRuntimeID
		}
		handle, ok := s.entityFromRuntimeID(pk.TargetEntityRuntimeID)
		if !ok {
			return nil
		}
		e, ok := handle.Entity(tx)
		if !ok {
			return nil
		}
		c.LeaveVehicle(e)
	case mc.InteractActionMouseOverEntity:
		// We don't need this action.
	case mc.InteractActionOpenInventory:
		s.openInventory(c)
	default:
		return fmt.Errorf("unexpected interact packet action %v", pk.ActionType)
	}
	return nil
}
