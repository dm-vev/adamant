package session

import (
	"fmt"
	"math"
	_ "unsafe"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// ResyncInventory resends the main and off-hand inventories after an unfulfilled client prediction.
func (s *Session) ResyncInventory() {
	s.sendInv(s.inv, mc.WindowIDInventory)
	s.sendInv(s.offHand, mc.WindowIDOffHand)
}

// sendInv sends the inventory passed to the client with the window ID.
func (s *Session) sendInv(inv *inventory.Inventory, windowID uint32) {
	pk := &mc.InventoryContentPacket{
		WindowID: windowID,
		Content:  make([]mc.ItemInstance, 0, inv.Size()),
	}
	for _, i := range inv.Slots() {
		pk.Content = append(pk.Content, instanceFromItem(s.tables(), i))
	}
	s.writePacket(pk)
}

// sendItem sends the item stack passed to the client with the window ID and slot passed.
func (s *Session) sendItem(item item.Stack, slot int, windowID uint32) {
	s.writePacket(&mc.InventorySlotPacket{
		WindowID: windowID,
		Slot:     uint32(slot),
		NewItem:  instanceFromItem(s.tables(), item),
	})
}

// smelter is an interface representing a block used to smelt items.
type smelter interface {
	// ResetExperience resets the collected experience of the smelter, and returns the amount of experience that was reset.
	ResetExperience() int
}

// invByID attempts to return an inventory by the ID passed. If found, the inventory is returned and the bool
// returned is true.
func (s *Session) invByID(id int32, tx *world.Tx) (*inventory.Inventory, bool) {
	switch id {
	case mc.ContainerCraftingInput, mc.ContainerCreatedOutput, mc.ContainerCursor:
		// UI inventory.
		return s.ui, true
	case mc.ContainerHotBar, mc.ContainerInventory, mc.ContainerCombinedHotBarAndInventory:
		// Hotbar 'inventory', rest of inventory, inventory when container is opened.
		return s.inv, true
	case mc.ContainerOffhand:
		return s.offHand, true
	case mc.ContainerArmor:
		// Armour inventory.
		return s.armour.Inventory(), true
	default:
		m := s.menu.Load()
		if m == nil {
			return nil, false
		}
		if id == mc.ContainerLevelEntity {
			// A menu without items of its own has no container 7: BDS
			// refuses an item put there (50).
			if w := s.window(); w != s.noWindow {
				return w, true
			}
			return nil, false
		}
		if m.Kind() == menu.Merchant {
			switch id {
			case mc.ContainerTradeIngredientOne, mc.ContainerTradeIngredientTwo,
				mc.ContainerTradeTwoIngredientOne, mc.ContainerTradeTwoIngredientTwo:
				// The trade screen's inputs are UI inventory slots 4 and 5.
				return s.ui, true
			}
		}
		if m.Entity() != nil {
			return nil, false
		}
		pos := m.Pos()
		switch id {
		case mc.ContainerShulkerBox:
			if _, ok := tx.Block(pos).(block.ShulkerBox); ok {
				return s.window(), true
			}
		case mc.ContainerBarrel:
			if _, barrel := tx.Block(pos).(block.Barrel); barrel {
				return s.window(), true
			}
		case mc.ContainerBeaconPayment:
			if _, beacon := tx.Block(pos).(block.Beacon); beacon {
				return s.ui, true
			}
		case mc.ContainerBrewingStandInput, mc.ContainerBrewingStandResult, mc.ContainerBrewingStandFuel:
			if _, brewingStand := tx.Block(pos).(block.BrewingStand); brewingStand {
				return s.window(), true
			}
		case mc.ContainerAnvilInput, mc.ContainerAnvilMaterial:
			if _, anvil := tx.Block(pos).(block.Anvil); anvil {
				return s.ui, true
			}
		case mc.ContainerSmithingTableTemplate, mc.ContainerSmithingTableInput, mc.ContainerSmithingTableMaterial:
			if _, smithing := tx.Block(pos).(block.SmithingTable); smithing {
				return s.ui, true
			}
		case mc.ContainerLoomInput, mc.ContainerLoomDye, mc.ContainerLoomMaterial:
			if _, loom := tx.Block(pos).(block.Loom); loom {
				return s.ui, true
			}
		case mc.ContainerCartographyInput, mc.ContainerCartographyAdditional:
			if _, ok := tx.Block(pos).(block.CartographyTable); ok {
				return s.ui, true
			}
		case mc.ContainerStonecutterInput:
			if _, ok := tx.Block(pos).(block.Stonecutter); ok {
				return s.ui, true
			}
		case mc.ContainerGrindstoneInput, mc.ContainerGrindstoneAdditional:
			if _, ok := tx.Block(pos).(block.Grindstone); ok {
				return s.ui, true
			}
		case mc.ContainerEnchantingInput, mc.ContainerEnchantingMaterial:
			if _, enchanting := tx.Block(pos).(block.EnchantingTable); enchanting {
				return s.ui, true
			}
		case mc.ContainerCrafterLevelEntity:
			if _, crafter := tx.Block(pos).(block.Crafter); crafter {
				return s.window(), true
			}
		case mc.ContainerFurnaceIngredient, mc.ContainerFurnaceFuel, mc.ContainerFurnaceResult,
			mc.ContainerBlastFurnaceIngredient, mc.ContainerSmokerIngredient:
			if _, ok := tx.Block(pos).(smelter); ok {
				return s.window(), true
			}
		}
	}
	return nil, false
}

// ViewInventories takes the inventories of the Controllable entity of the
// session, and returns what the client is shown of their changes: the slots
// changed, unless an item stack request changed them, whose response shows
// them, the slots of an ender chest open, and the whole UI inventory.
func (s *Session) ViewInventories(tx *world.Tx, c Controllable, inv menu.Inventories, heldSlot *uint32) client.InventoryViews {
	// Store the held slot pointer before the slot functions are installed to avoid races with concurrent inventory
	// updates.
	s.heldSlot.Store(heldSlot)
	// The teardown unlists c from the view layers of other sessions.
	s.control.Store(&c)
	s.inv, s.offHand, s.enderChest, s.ui, s.armour = inv.Main, inv.OffHand, inv.EnderChest, inv.UI, inv.Armour
	if unlocker, ok := c.(interface{ UnlockedRecipeIDs() []string }); ok {
		s.SendInitiallyUnlockedRecipes(unlocker.UnlockedRecipeIDs())
	}
	return client.InventoryViews{
		Main: func(slot int, _, after item.Stack) {
			if !s.inTransaction.Load() {
				s.sendItem(after, slot, mc.WindowIDInventory)
			}
		},
		OffHand: func(int, item.Stack, item.Stack) {
			if !s.inTransaction.Load() {
				i, _ := s.offHand.Item(0)
				s.writePacket(&mc.InventoryContentPacket{
					WindowID: mc.WindowIDOffHand,
					Content:  []mc.ItemInstance{instanceFromItem(s.tables(), i)},
				})
			}
		},
		EnderChest: s.enderChestView(c),
		UI: func(int, item.Stack, item.Stack) {
			s.sendInv(s.ui, mc.WindowIDUI)
		},
		Armour: func(slot int, _, after item.Stack) {
			if !s.inTransaction.Load() {
				s.sendItem(after, slot, mc.WindowIDArmour)
			}
		},
	}
}

// enderChestView returns what the client is shown of a change of the ender
// chest of c: the slot changed, if an ender chest is open.
func (s *Session) enderChestView(c Controllable) inventory.SlotFunc {
	return func(slot int, _, after item.Stack) {
		m := s.menu.Load()
		if s.inTransaction.Load() || m == nil {
			return
		}
		pos := m.Pos()
		// Check container state inside the transaction to avoid sending updates after the chest is closed or moved.
		c.H().Do(func(tx *world.Tx, _ world.Entity) {
			current := s.menu.Load()
			if current == nil || current.Pos() != pos {
				return
			}
			if _, ok := tx.Block(pos).(block.EnderChest); ok {
				s.ViewSlotChange(slot, after)
			}
		})
	}
}

// SendHeldSlot sends the currently held hotbar slot.
func (s *Session) SendHeldSlot(slot int, c Controllable, force bool) {
	if s.changingSlot.Load() && !force {
		return
	}
	mainHand, _ := c.HeldItems()
	s.writePacket(&mc.MobEquipmentPacket{
		EntityRuntimeID: selfEntityRuntimeID,
		NewItem:         instanceFromItem(s.tables(), mainHand),
		InventorySlot:   byte(slot),
		HotBarSlot:      byte(slot),
	})
}

// VerifyAndSetHeldSlot verifies if the slot passed is a valid hotbar slot and
// if the expected item.Stack is in it. Afterwards, it changes the held slot
// of the player.
func (s *Session) VerifyAndSetHeldSlot(slot int, expected item.Stack, c Controllable) error {
	if err := s.VerifySlot(slot, expected); err != nil {
		return err
	}
	s.changingSlot.Store(true)
	defer s.changingSlot.Store(false)
	return c.SetHeldSlot(slot)
}

// VerifySlot verifies if the slot passed is a valid hotbar slot and if the
// expected item.Stack is in it.
func (s *Session) VerifySlot(slot int, expected item.Stack) error {
	// The slot that the player might have selected must be within the hotbar:
	// The held item cannot be in a different place in the inventory.
	if slot < 0 || slot > 8 {
		return fmt.Errorf("slot exceeds hotbar range 0-8: slot is %v", slot)
	}
	clientSideItem := expected
	actual, _ := s.inv.Item(slot)
	if clientCompass, clientOK := clientSideItem.Item().(item.Compass); clientOK {
		if serverCompass, serverOK := actual.Item().(item.Compass); serverOK && clientCompass.TrackingHandle != serverCompass.TrackingHandle {
			clientSideItem = clientSideItem.WithItem(serverCompass)
		}
	}

	// The item the client claims to have must be identical to the one we have
	// registered server-side.
	if !clientSideItem.Equal(actual) {
		s.sendItem(actual, slot, mc.WindowIDInventory)
		// Only ever debug these as they are frequent and expected to happen
		// whenever client and server get out of sync.
		s.conf.Log.Debug("verify slot: client-side item was not equal to server-side item", "client-held", clientSideItem.String(), "server-held", actual.String())
	}
	return nil
}

// stackFromItem converts an item.Stack to its network ItemStack representation,
// with the item and block IDs of the tables passed.
func stackFromItem(w *wireTables, it item.Stack) mc.ItemStack {
	if it.Empty() {
		return mc.ItemStack{}
	}

	var blockRuntimeID uint32
	if b, ok := it.Item().(world.Block); ok {
		blockRuntimeID = w.blockID(b)
	}

	rid, meta, _ := w.itemRuntimeID(it.Item())

	return mc.ItemStack{
		NetworkID:      rid,
		MetadataValue:  uint32(meta),
		Count:          uint16(it.Count()),
		BlockRuntimeID: int32(blockRuntimeID),
		NBTData:        networkItemNBT(it),
	}
}

// stackToItem converts a network ItemStack representation back to an item.Stack,
// by the item and block IDs of the tables passed.
func stackToItem(w *wireTables, it mc.ItemStack) item.Stack {
	br := w.br
	t, ok := w.itemByRuntimeID(it.NetworkID, int16(it.MetadataValue))
	if !ok {
		t = block.Air{}
	}
	if it.BlockRuntimeID != 0 {
		// Clients name the block of a block item by its network block hash. An
		// unknown hash or a block that is no item leaves air.
		t = block.Air{}
		if rid, known := w.hashes().hashToRuntimeID(uint32(it.BlockRuntimeID)); known {
			if b, found := br.BlockByRuntimeID(rid); found {
				if blockItem, isItem := b.(world.Item); isItem {
					t = blockItem
				}
			}
		}
	}
	//noinspection SpellCheckingInspection
	if compass, ok := t.(item.Compass); ok {
		// Decode even without NBT so the lodestone-compass registry variant cannot leak its registration handle.
		t = compass.DecodeNBT(it.NBTData).(world.Item)
	} else if nbter, ok := t.(world.NBTer); ok && len(it.NBTData) != 0 {
		t = world.DecodeNBT(nbter, it.NBTData, br).(world.Item)
	}
	s := item.NewStack(t, int(it.Count))
	return readNetworkItemNBT(it.NBTData, s)
}

// instanceFromItem converts an item.Stack to its network ItemInstance representation.
func instanceFromItem(w *wireTables, it item.Stack) mc.ItemInstance {
	return mc.ItemInstance{
		StackNetworkID: item_id(it),
		Stack:          stackFromItem(w, it),
	}
}

// stacksToRecipeStacks converts a list of item.Stacks to their protocol representation with damage stripped for recipes.
func stacksToRecipeStacks(w *wireTables, inputs []item.Stack) []mc.ItemStack {
	items := make([]mc.ItemStack, 0, len(inputs))
	for _, i := range inputs {
		items = append(items, deleteDamage(stackFromItem(w, i)))
	}
	return items
}

// stacksToIngredientItems converts a list of item.Stacks to recipe ingredient items used over the network.
func stacksToIngredientItems(inputs []recipe.Item) []mc.ItemDescriptorCount {
	items := make([]mc.ItemDescriptorCount, 0, len(inputs))
	for _, i := range inputs {
		var d mc.ItemDescriptor = &mc.InvalidItemDescriptor{}
		switch i := i.(type) {
		case item.Stack:
			if i.Empty() {
				items = append(items, mc.ItemDescriptorCount{Descriptor: &mc.InvalidItemDescriptor{}})
				continue
			}
			name, meta := i.Item().EncodeItem()
			if _, ok := i.Value("variants"); ok {
				meta = math.MaxInt16 // Used to indicate that the item has multiple selectable variants.
			}
			d = &mc.DefaultItemDescriptor{
				Name:          name,
				MetadataValue: int32(meta),
			}
		case recipe.ItemTag:
			d = &mc.ItemTagItemDescriptor{Tag: i.Tag()}
		}
		items = append(items, mc.ItemDescriptorCount{
			Descriptor: d,
			Count:      int32(i.Count()),
		})
	}
	return items
}

// deleteDamage strips the damage from a protocol item.
func deleteDamage(st mc.ItemStack) mc.ItemStack {
	delete(st.NBTData, "Damage")
	return st
}

// noinspection ALL
//
//go:linkname item_id github.com/df-mc/dragonfly/server/item.id
func item_id(s item.Stack) int32
