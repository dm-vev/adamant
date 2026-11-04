package session

import (
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
)

// beaconTestViewer is the viewer of the menus of TestBeaconPaymentByType.
type beaconTestViewer struct{ world.NopViewer }

func (*beaconTestViewer) ViewSlotChange(int, item.Stack)                                 {}
func (*beaconTestViewer) ViewCrafterSlotState(int, bool)                                 {}
func (*beaconTestViewer) ViewEnchantmentOptions([3]menu.EnchantmentOption)               {}
func (*beaconTestViewer) ViewTrades(*menu.Menu, entity.Merchant, []entity.MerchantOffer) {}

// TestBeaconPaymentByType checks that a beacon payment of every effect ID,
// as the primary and as the secondary effect, at every level of the
// pyramid, gets the same verdict from the IDs the session turns into
// effect types and menu.Tx.PayBeacon as the session gave by ID before
// (testdata/frozen/beacon.txt).
func TestBeaconPaymentByType(t *testing.T) {
	frozen := &frozenSweep{name: "beacon"}
	pos := cube.Pos{0, 64, 0}
	for level := 0; level <= 4; level++ {
		w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
		<-w.Exec(func(tx *world.Tx) {
			for l := 1; l <= level; l++ {
				for x := -l; x <= l; x++ {
					for z := -l; z <= l; z++ {
						tx.SetBlock(pos.Add(cube.Pos{x, -l, z}), block.Iron{}, nil)
					}
				}
			}
			tx.SetBlock(pos, block.Beacon{}, nil)
			tx.Block(pos).(block.Beacon).Tick(0, pos, tx)
			beacon := tx.Block(pos).(block.Beacon)
			if beacon.Level() != level {
				t.Fatalf("the beacon is of level %d, want %d", beacon.Level(), level)
			}
			m := menu.OpenBlock(tx, pos, &beaconTestViewer{}, nil)
			for id := int32(-2); id <= 40; id++ {
				for _, ids := range [][2]int32{{id, 0}, {1, id}} {
					paid := false
					primary, errP := beaconEffect(ids[0])
					secondary, errS := beaconEffect(ids[1])
					if errP == nil && errS == nil {
						ui := inventory.New(54, nil)
						_ = ui.SetItem(menu.UIBeaconPayment, item.NewStack(item.IronIngot{}, 1))
						b := menu.Begin(menu.Opts{Tx: tx, Menu: m, Inv: menu.Inventories{UI: ui}, Name: func(menu.Slot, menu.Role) menu.Name { return 0 }})
						paid = b.PayBeacon(primary, secondary) == nil
						b.Rollback()
						tx.SetBlock(pos, beacon, nil)
					}
					frozen.add(fmt.Sprintf("%d %d %d", level, ids[0], ids[1]), fmt.Sprint(paid))
				}
			}
		})
		_ = w.Close()
	}
	frozen.check(t)
}
