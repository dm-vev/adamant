package menu

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/item/creative"
)

// CreateCreative crafts a full stack of the creative item of index i of
// creative.Items(). Only a player with the creative inventory may.
func (t *Tx) CreateCreative(i int) error {
	if err := t.refused(); err != nil {
		return err
	}
	if !t.o.Owner.GameMode().CreativeInventory() {
		return fmt.Errorf("can only craft creative items in gamemode creative/spectator")
	}
	items := creative.Items()
	if i < 0 || i >= len(items) {
		return fmt.Errorf("creative item %v does not exist", i)
	}
	it := items[i].Stack
	it = it.Grow(it.MaxCount() - 1)
	return t.createResults(it)
}
