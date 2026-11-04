package player

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/menu"
)

// SetCrafterSlotDisabled disables or enables the slot of the crafter at pos,
// as the player toggles it in the crafter it has open. It fails if the
// player has no crafter menu open at pos, if the slot is not one of the nine
// of a crafter, if the crafter is out of the reach of the player or if no
// crafter is at pos.
//
// The viewers of the crafter are shown the slot changed. A slot that holds
// an item, or is already as asked, does not change, and the client of the
// player is shown it again as it is.
func (p *Player) SetCrafterSlotDisabled(pos cube.Pos, slot int, disabled bool) error {
	if m := p.Menu(); m == nil || m.Kind() != menu.Crafter || m.Pos() != pos {
		return fmt.Errorf("no matching Crafter container opened")
	}
	if slot < 0 || slot >= 9 || !p.CanReach(pos.Vec3Middle()) {
		return fmt.Errorf("invalid or unreachable Crafter slot request")
	}
	crafter, ok := p.tx.Block(pos).(block.Crafter)
	if !ok {
		return fmt.Errorf("Crafter block no longer exists")
	}
	after, changed := crafter.SetSlotDisabled(slot, disabled)
	if !changed {
		p.c.ViewCrafterSlotState(slot, crafter.DisabledSlots&(1<<slot) != 0)
		return nil
	}
	p.tx.SetBlock(pos, after, nil)
	after.NotifySlotState(slot)
	return nil
}
