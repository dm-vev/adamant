package session

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// CrafterSlotToggleHandler validates and applies native Crafter slot-state requests.
type CrafterSlotToggleHandler struct{}

func (CrafterSlotToggleHandler) Handle(p packet.Packet, s *Session, tx *world.Tx, c Controllable) error {
	pk := p.(*packet.PlayerToggleCrafterSlotRequest)
	pos := cube.Pos{int(pk.PosX), int(pk.PosY), int(pk.PosZ)}
	opened := s.openedPos.Load()
	if !s.containerOpened.Load() || opened == nil || *opened != pos || s.openedContainerID.Load() != uint32(protocol.ContainerTypeCrafter) {
		return fmt.Errorf("no matching Crafter container opened")
	}
	if pk.Slot >= 9 || !canReach(c, pos.Vec3Middle()) {
		return fmt.Errorf("invalid or unreachable Crafter slot request")
	}
	crafter, ok := tx.Block(pos).(block.Crafter)
	if !ok {
		return fmt.Errorf("Crafter block no longer exists")
	}
	after, changed := crafter.SetSlotDisabled(int(pk.Slot), pk.Disabled)
	if !changed {
		s.ViewCrafterSlotState(int(pk.Slot), crafter.DisabledSlots&(1<<pk.Slot) != 0)
		return nil
	}
	tx.SetBlock(pos, after, nil)
	after.NotifySlotState(int(pk.Slot))
	return nil
}
