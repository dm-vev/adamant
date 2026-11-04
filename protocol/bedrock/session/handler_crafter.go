package session

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// CrafterSlotToggleHandler validates and applies native Crafter slot-state requests.
type CrafterSlotToggleHandler struct{}

func (CrafterSlotToggleHandler) Handle(p proto.Packet, s *Session, _ *world.Tx, c Controllable) error {
	pk := p.(*v2193.PlayerToggleCrafterSlotRequestPacket)
	pos := cube.Pos{int(pk.PosX), int(pk.PosY), int(pk.PosZ)}
	if m := s.menu.Load(); m == nil || m.Pos() != pos || s.openedContainerID.Load() != uint32(v2193.ContainerTypeCrafter) {
		return fmt.Errorf("no matching Crafter container opened")
	}
	// The player checks the slot, the reach and the crafter, and toggles it.
	return c.SetCrafterSlotDisabled(pos, int(pk.Slot), pk.Disabled)
}
