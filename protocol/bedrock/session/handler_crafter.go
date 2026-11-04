package session

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// CrafterSlotToggleHandler validates and applies native Crafter slot-state requests.
type CrafterSlotToggleHandler struct{}

func (CrafterSlotToggleHandler) Handle(p proto.Packet, _ *Session, _ *world.Tx, c Controllable) error {
	pk := p.(*v2193.PlayerToggleCrafterSlotRequestPacket)
	pos := cube.Pos{int(pk.PosX), int(pk.PosY), int(pk.PosZ)}
	// The player checks its menu, the slot, the reach and the crafter, and
	// toggles it.
	return c.SetCrafterSlotDisabled(pos, int(pk.Slot), pk.Disabled)
}
