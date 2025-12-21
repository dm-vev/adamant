package session

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// InteractHandler handles the packet.Interact.
type InteractHandler struct{}

// Handle ...
func (h *InteractHandler) Handle(p packet.Packet, s *Session, _ *world.Tx, c Controllable) error {
	pk := p.(*packet.Interact)
	pos := c.Position()

	switch pk.ActionType {
	case packet.InteractActionLeaveVehicle:
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
	case packet.InteractActionMouseOverEntity:
		// We don't need this action.
	case packet.InteractActionOpenInventory:
		if s.invOpened {
			// When there is latency, this might end up being sent multiple times. If we send a ContainerOpen
			// multiple times, the client crashes.
			return nil
		}
		s.invOpened = true
		s.writePacket(&packet.ContainerOpen{
			WindowID:                0,
			ContainerType:           0xff,
			ContainerEntityUniqueID: -1,
			ContainerPosition: protocol.BlockPos{
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
