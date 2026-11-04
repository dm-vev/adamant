package player

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
)

// crafterSlotViewer is a client that shows the slots of the crafter its
// player has open, as the viewers of a crafter do (block.Crafter).
type crafterSlotViewer interface {
	ViewCrafterSlotState(slot int, disabled bool)
}

// SetCrafterSlotDisabled disables or enables the slot of the crafter at pos,
// as the player toggles it in the crafter it has open, which its client
// checks. It fails if the slot is not one of the nine of a crafter, if the
// crafter is out of the reach of the player or if no crafter is at pos.
//
// The viewers of the crafter are shown the slot changed. A slot that holds
// an item, or is already as asked, does not change, and the client of the
// player is shown it again as it is.
func (p *Player) SetCrafterSlotDisabled(pos cube.Pos, slot int, disabled bool) error {
	if slot < 0 || slot >= 9 || !p.CanReach(pos.Vec3Middle()) {
		return fmt.Errorf("invalid or unreachable Crafter slot request")
	}
	crafter, ok := p.tx.Block(pos).(block.Crafter)
	if !ok {
		return fmt.Errorf("Crafter block no longer exists")
	}
	after, changed := crafter.SetSlotDisabled(slot, disabled)
	if !changed {
		if v, ok := p.client().(crafterSlotViewer); ok {
			v.ViewCrafterSlotState(slot, crafter.DisabledSlots&(1<<slot) != 0)
		}
		return nil
	}
	p.tx.SetBlock(pos, after, nil)
	after.NotifySlotState(slot)
	return nil
}
