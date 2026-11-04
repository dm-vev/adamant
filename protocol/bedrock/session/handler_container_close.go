package session

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// ContainerCloseHandler handles the ContainerClose packet.
type ContainerCloseHandler struct{}

// Handle ...
func (h *ContainerCloseHandler) Handle(p proto.Packet, s *Session, tx *world.Tx, c Controllable) error {
	pk := p.(*mc.ContainerClosePacket)

	c.MoveItemsToInventory()

	var containerType byte
	switch pk.WindowID {
	case 0:
		// Closing of the normal inventory.
		s.invOpened = false
	case byte(s.openedWindowID.Load()):
		containerType = byte(s.openedContainerID.Load())
		c.CloseMenu(true)
	case 0xff:
		// Sent when an inventory/container is opened at the same time as chat.
		s.invOpened = false
		if s.menu.Load() != nil {
			c.CloseMenu(false)
		}
		return nil
	default:
		containerType = pk.ContainerType
	}
	s.writePacket(&mc.ContainerClosePacket{
		WindowID:      pk.WindowID,
		ContainerType: containerType,
	})
	return nil
}
