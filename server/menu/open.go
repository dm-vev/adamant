package menu

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
)

// OpenBlock opens the menu of the block at pos for a player viewed through v.
// A block.Container gets v added as its viewer, which may change the block
// (a pair of chests), and the menu is of the block read after that. An
// ender chest counts an opener (block.EnderChest.AddViewer) and shows ender,
// the ender chest of the player. Lectern and Other register nothing.
func OpenBlock(tx *world.Tx, pos cube.Pos, v Viewer, ender *inventory.Inventory) *Menu {
	m := &Menu{pos: pos, viewer: v}
	b := tx.Block(pos)
	if container, ok := b.(block.Container); ok {
		container.AddViewer(v, tx, pos) // Paired chests might update the block here.
		container = tx.Block(pos).(block.Container)
		m.kind, m.inv = containerKind(container), container.Inventory(tx, pos)
		return m
	}
	m.kind, m.inv = blockKind(b), inventory.New(1, nil)
	if enderChest, ok := b.(block.EnderChest); ok {
		enderChest.AddViewer(tx, pos)
		m.inv = ender
	}
	return m
}

// containerKind returns the kind of the menu of a block container.
func containerKind(b block.Container) Kind {
	switch b.(type) {
	case block.Furnace:
		return Furnace
	case block.BrewingStand:
		return BrewingStand
	case block.BlastFurnace:
		return BlastFurnace
	case block.Smoker:
		return Smoker
	case block.Hopper:
		return Hopper
	case block.Crafter:
		return Crafter
	}
	return Generic
}

// blockKind returns the kind of the menu of a block that is no container.
func blockKind(b world.Block) Kind {
	switch b.(type) {
	case block.CraftingTable:
		return Crafting
	case block.EnchantingTable:
		return Enchantment
	case block.Anvil:
		return Anvil
	case block.Beacon:
		return Beacon
	case block.Lectern:
		return Lectern
	case block.Loom:
		return Loom
	case block.Grindstone:
		return Grindstone
	case block.CartographyTable:
		return Cartography
	case block.Stonecutter:
		return Stonecutter
	case block.SmithingTable:
		return Smithing
	case block.EnderChest:
		return EnderChest
	}
	return Other
}

// OpenEntity opens inv, the inventory of e, as a container of the kind
// passed, and adds v as a viewer of e if e keeps viewers.
func OpenEntity(e world.Entity, inv *inventory.Inventory, kind entity.ContainerKind, v Viewer) *Menu {
	if viewers, ok := e.(interface {
		AddViewer(v block.ContainerViewer)
	}); ok {
		viewers.AddViewer(v)
	}
	return &Menu{kind: Entity, pos: standingPos(e), handle: e.H(), ekind: kind, inv: inv, viewer: v}
}

// OpenMerchant opens the trades of merchant, the entity e, for the player of
// the handle user. A TradingMerchant must agree to trade (StartTrading);
// OpenMerchant reports false if it does not.
func OpenMerchant(e world.Entity, merchant entity.Merchant, user *world.EntityHandle) (*Menu, bool) {
	if trader, ok := merchant.(entity.TradingMerchant); ok && !trader.StartTrading(user) {
		return nil, false
	}
	return &Menu{kind: Merchant, pos: standingPos(e), handle: e.H(), inv: inventory.New(1, nil)}, true
}

// standingPos returns the block e stands in, its coordinates truncated
// towards zero as the session truncated them.
func standingPos(e world.Entity) cube.Pos {
	pos := e.Position()
	return cube.Pos{int(pos[0]), int(pos[1]), int(pos[2])}
}

// Close releases what opening the menu took, in tx. For an entity, it removes
// the viewer and stops the trade of a TradingMerchant. For a block, it
// removes the viewer from the block now at the position: block.Container.RemoveViewer
// or block.EnderChest.RemoveViewer, as the session did, whatever the menu
// was opened as.
func (m *Menu) Close(tx *world.Tx, user *world.EntityHandle) {
	if m.handle != nil {
		ent, ok := m.handle.Entity(tx)
		if !ok {
			return
		}
		if viewers, ok := ent.(interface {
			RemoveViewer(v block.ContainerViewer)
		}); ok {
			viewers.RemoveViewer(m.viewer)
		}
		if trader, ok := ent.(entity.TradingMerchant); ok {
			trader.StopTrading(user)
		}
		return
	}
	b := tx.Block(m.pos)
	if container, ok := b.(block.Container); ok {
		container.RemoveViewer(m.viewer, tx, m.pos)
	} else if enderChest, ok := b.(block.EnderChest); ok {
		enderChest.RemoveViewer(tx, m.pos)
	}
}
