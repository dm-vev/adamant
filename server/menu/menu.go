// Package menu holds the menus of the game: what a player has open when it
// moves items between its inventories and a block or an entity, crafts,
// enchants, repairs, grinds, smiths, weaves, cuts, pays a beacon or trades.
// It is modelled on the container menus of Java Edition
// (AbstractContainerMenu, EnchantmentMenu, AnvilMenu, GrindstoneMenu,
// SmithingMenu, LoomMenu, StonecutterMenu, BeaconMenu, MerchantMenu,
// CraftingMenu, InventoryMenu, CrafterMenu, the furnace menus and the
// generic containers).
//
// A protocol adapter tells the game what its client does in a menu as the
// operations of a Tx, and shows its client what the menu tells its Viewer.
// The inputs of menus are kept in the UI inventory of the player at the UI
// slots below, the layout the game has always used for them. An adapter maps
// its own slots onto them.
//
// The package imports no protocol.
package menu

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
)

// The slots of the UI inventory of a player (54 slots) that hold the cursor,
// the crafting grids and the inputs and result of menus while they are open.
// MoveItemsToInventory empties them.
const (
	UICursor                                    = 0
	UIAnvilInput, UIAnvilMaterial               = 1, 2
	UIStonecutterInput                          = 3
	UITradeInputA, UITradeInputB                = 4, 5
	UILoomBanner, UILoomDye, UILoomPattern      = 9, 10, 11
	UICartographyInput, UICartographyAdditional = 12, 13
	UIEnchantingInput, UIEnchantingLapis        = 14, 15
	UIGrindstoneFirst, UIGrindstoneSecond       = 16, 17
	UIBeaconPayment                             = 27
	// UICraftingSmall is the first of the four slots of the 2x2 grid of the
	// inventory, UICraftingLarge the first of the nine of a crafting table.
	UICraftingSmall, UICraftingLarge                        = 28, 32
	UIResult                                                = 50
	UISmithingInput, UISmithingMaterial, UISmithingTemplate = 51, 52, 53
)

// Kind is what a menu is open for. An adapter shows each kind as its
// protocol shows that screen.
type Kind uint8

const (
	// Generic is a block container without a menu of its own: a chest,
	// trapped chest, copper chest, barrel, shulker box, dispenser, dropper
	// and any other block.Container not named below.
	Generic Kind = iota + 1
	EnderChest
	Furnace
	BlastFurnace
	Smoker
	BrewingStand
	Hopper
	Crafter
	// Crafting is a crafting table.
	Crafting
	Enchantment
	Anvil
	Beacon
	Loom
	Grindstone
	Cartography
	Stonecutter
	Smithing
	// Lectern is the screen of a lectern. It holds no state: the player
	// keeps no menu open for it, as the client sends no close for it.
	Lectern
	// Other is a block that opened a screen the game models no menu for.
	Other
	// Entity is the inventory of an entity, of the ContainerKind of the menu.
	Entity
	Merchant
)

// Menu is a menu a player has open. It is bound to one player, and to a block
// or an entity, from Open to Close. A Menu is valid across transactions: it
// holds positions and handles, never a *world.Tx.
type Menu struct {
	kind   Kind
	pos    cube.Pos
	handle *world.EntityHandle  // Entity and Merchant
	ekind  entity.ContainerKind // Entity
	inv    *inventory.Inventory // see Inventory
	viewer Viewer
}

// Kind returns what the menu is open for.
func (m *Menu) Kind() Kind {
	return m.kind
}

// Pos returns the block the menu is open for. For an entity or a merchant it
// is the block the entity stood in when the menu opened, as before.
func (m *Menu) Pos() cube.Pos {
	return m.pos
}

// Entity returns the handle of the entity the menu is open for, nil for a
// block.
func (m *Menu) Entity() *world.EntityHandle {
	return m.handle
}

// ContainerKind returns the kind of container of an Entity menu.
func (m *Menu) ContainerKind() entity.ContainerKind {
	return m.ekind
}

// Inventory returns the inventory the menu shows: that of the block container
// or of the entity, the ender chest of the player for EnderChest, and a
// placeholder of one slot for any other kind. The placeholder exists as it
// did in the session.
func (m *Menu) Inventory() *inventory.Inventory {
	return m.inv
}

const (
	gridSizeSmall, gridSizeLarge = 4, 9
)

// Grid returns the first UI slot and the size of the crafting grid of the
// menu: the 3x3 grid for Crafting, the 2x2 grid of the inventory otherwise
// (also when no menu is open: Grid of a nil *Menu).
func (m *Menu) Grid() (first, size int) {
	if m != nil && m.kind == Crafting {
		return UICraftingLarge, gridSizeLarge
	}
	return UICraftingSmall, gridSizeSmall
}

// Merchant returns the merchant entity of a Merchant menu, and the merchant,
// in tx. It fails if the menu is no Merchant, or the entity left or is no
// merchant any more. Merchant of a nil *Menu fails as no Merchant.
func (m *Menu) Merchant(tx *world.Tx) (world.Entity, entity.Merchant, error) {
	if m == nil || m.kind != Merchant {
		return nil, nil, fmt.Errorf("no merchant window is open")
	}
	target, ok := m.handle.Entity(tx)
	if !ok {
		return nil, nil, fmt.Errorf("merchant is no longer in the world")
	}
	merchant, ok := target.(entity.Merchant)
	if !ok {
		return nil, nil, fmt.Errorf("opened entity is not a merchant")
	}
	return target, merchant, nil
}

// Viewer is what a menu shows its player's client through, and what it
// registers with the containers it opens. The client.Client of a player
// implements it. A Viewer must be comparable: containers keep their viewers
// as map keys.
type Viewer interface {
	block.ContainerViewer
	// ViewCrafterSlotState shows whether a slot of an open crafter is disabled.
	ViewCrafterSlotState(slot int, disabled bool)
	// ViewEnchantmentOptions shows the three options of an open enchanting table.
	ViewEnchantmentOptions(options [3]EnchantmentOption)
	// ViewTrades shows offers, the offers of merchant, the merchant of the
	// Merchant menu m, as they are now.
	ViewTrades(m *Menu, merchant entity.Merchant, offers []entity.MerchantOffer)
}

// EnchantmentOption is an option of an enchanting table.
type EnchantmentOption struct {
	// Cost is the level the player needs; the option costs its index + 1.
	Cost         int
	Enchantments []item.Enchantment
}

// Inventories are the inventories of a player that menus move items in.
type Inventories struct {
	Main, OffHand, EnderChest, UI *inventory.Inventory
	Armour                        *inventory.Armour
}
