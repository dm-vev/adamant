package menu

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
)

// PayBeacon sets the effects of the beacon of the menu, paid with the item
// in its payment slot. nil is no effect, which leaves the effect the beacon
// has.
//
// The beacon allows speed and haste from level 1 of its pyramid,
// resistance and jump boost from level 2, strength from level 3 and
// regeneration from level 4, and a secondary effect only at level 4. The
// payment is taken.
func (t *Tx) PayBeacon(primary, secondary effect.LastingType) error {
	slot := Slot{Inv: t.o.Inv.UI, Index: UIBeaconPayment}
	// First check if there actually is a beacon opened.
	m := t.o.Menu
	if m == nil {
		return fmt.Errorf("no beacon container opened")
	}
	pos := m.Pos()
	beacon, ok := t.o.Tx.Block(pos).(block.Beacon)
	if !ok {
		return fmt.Errorf("no beacon container opened")
	}

	// Check if the item present in the beacon slot is valid.
	payment, _ := t.Item(slot)
	if payable, ok := payment.Item().(item.BeaconPayment); !ok || !payable.PayableForBeacon() {
		return fmt.Errorf("item %#v in beacon slot cannot be used as payment", payment)
	}

	// Check if the effects are valid and allowed for the beacon's level.
	if !validBeaconEffect(primary, beacon) {
		return fmt.Errorf("primary effect selected is not allowed: %v for level %v", primary, beacon.Level())
	} else if !validBeaconEffect(secondary, beacon) || (beacon.Level() < 4 && secondary != nil) {
		return fmt.Errorf("secondary effect selected is not allowed: %v for level %v", secondary, beacon.Level())
	}

	if primary != nil {
		beacon.Primary = primary
	}
	if secondary != nil {
		beacon.Secondary = secondary
	}
	t.o.Tx.SetBlock(pos, beacon, nil)

	_ = t.set(t.named(slot, RoleBeaconPayment), item.Stack{})
	return nil
}

// validBeaconEffect checks if the effect passed is allowed for the level of
// the beacon. nil, no effect, always is.
func validBeaconEffect(e effect.LastingType, beacon block.Beacon) bool {
	switch e {
	case effect.Speed, effect.Haste:
		return beacon.Level() >= 1
	case effect.JumpBoost, effect.Resistance:
		return beacon.Level() >= 2
	case effect.Strength:
		return beacon.Level() >= 3
	case effect.Regeneration:
		return beacon.Level() >= 4
	case nil:
		return true
	}
	return false
}
