package player

import (
	"sync/atomic"

	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/entity"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/item/inventory"
	"github.com/dm-vev/adamant/server/menu"
	"github.com/dm-vev/adamant/server/world"
	"github.com/dm-vev/adamant/server/world/sound"
)

// viewInventories hands the client of p the inventories of p, as p opens
// in tx, and installs their slot functions. They show the viewers of p the
// items it holds and the armour it wears, and the options of the enchanting
// table it has open, and then call what the client shows of the change. The
// broadcasts run in a transaction of the entity of p, scheduled with Do
// rather than in the transaction the change was made in, which may have
// ended or may not be the transaction of a slot function that runs off one.
func viewInventories(tx *world.Tx, p *Player) {
	pd, h := p.playerData, p.H()
	views := pd.c.ViewInventories(tx, p, menu.Inventories{Main: pd.inv, OffHand: pd.offHand, EnderChest: pd.enderChest, UI: pd.ui, Armour: pd.armour}, pd.heldSlot)

	pd.inv.SlotFunc(func(slot int, before, after item.Stack) {
		pd.invVersion.Add(1)
		pd.trackMapSlot(before, after)
		if slot == int(atomic.LoadUint32(pd.heldSlot)) {
			h.Do(showHeldItems)
		}
		view(views.Main, slot, before, after)
	})
	pd.offHand.SlotFunc(func(slot int, before, after item.Stack) {
		pd.trackMapSlot(before, after)
		h.Do(showHeldItems)
		view(views.OffHand, slot, before, after)
	})
	pd.enderChest.SlotFunc(func(slot int, before, after item.Stack) {
		view(views.EnderChest, slot, before, after)
	})
	pd.armour.Inventory().SlotFunc(func(slot int, before, after item.Stack) {
		pd.trackMapSlot(before, after)
		if slot == 0 {
			pd.refreshTurtleHelmet()
		}
		view(views.Armour, slot, before, after)
		if before.Comparable(after) && before.Empty() == after.Empty() {
			// Only show the armour if the type of the item changed.
			return
		}
		// The item put on with an operation of a menu is heard put on.
		equipped := !after.Empty() && pd.menuTx.Load()
		h.Do(func(tx *world.Tx, e world.Entity) {
			world.BroadcastEntityArmour(tx, e)
			if equipped {
				tx.PlaySound(entity.EyePosition(e), sound.EquipItem{Item: after.Item()})
			}
		})
	})
	pd.ui.SlotFunc(func(slot int, before, after item.Stack) {
		if m := pd.menu.Load(); slot == menu.UIEnchantingInput && m != nil {
			pos := m.Pos()
			h.Do(func(tx *world.Tx, e world.Entity) {
				if _, enchanting := tx.Block(pos).(block.EnchantingTable); !enchanting {
					return
				}
				if options, ok := menu.EnchantmentOptions(tx, e.(*Player).EnchantmentSeed(), pos, after); ok {
					pd.c.ViewEnchantmentOptions(options)
				}
			})
		}
		view(views.UI, slot, before, after)
	})
}

// showHeldItems shows the viewers of e, a player, the items it holds.
func showHeldItems(tx *world.Tx, e world.Entity) {
	world.BroadcastEntityItems(tx, e)
}

// view calls f, what a client shows of a change of a slot, unless it is nil.
func view(f inventory.SlotFunc, slot int, before, after item.Stack) {
	if f != nil {
		f(slot, before, after)
	}
}
