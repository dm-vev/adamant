package session

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// InteractHandler handles the Interact packet.
type InteractHandler struct{}

// Handle ...
func (h *InteractHandler) Handle(p proto.Packet, s *Session, tx *world.Tx, c Controllable) error {
	pk := p.(*v2193.InteractPacket)
	pos := c.Position()

	switch pk.ActionType {
	case v2193.InteractActionLeaveVehicle:
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
		if dismountable, ok := e.(interface {
			Dismount(tx *world.Tx, passenger world.Entity)
		}); ok {
			dismountable.Dismount(tx, c)
		}
	case v2193.InteractActionMouseOverEntity:
		// We don't need this action.
	case v2193.InteractActionOpenInventory:
		if s.invOpened {
			// When there is latency, this might end up being sent multiple times. If we send a ContainerOpen
			// multiple times, the client crashes.
			return nil
		}
		s.invOpened = true
		s.writePacket(&v2193.ContainerOpenPacket{
			WindowID:                0,
			ContainerType:           0xff,
			ContainerEntityUniqueID: -1,
			ContainerPosition: v2193.BlockPos{
				int32(pos[0]),
				int32(pos[1]),
				int32(pos[2]),
			},
		})
	default:
		return fmt.Errorf("unexpected interact packet action %v", pk.ActionType)
	}
	return nil
}
