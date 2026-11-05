package session

import (
	"bytes"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/net/nbt"
)

// ViewFurnaceUpdate updates a furnace for the associated session based on previous times.
// Fuel values are UI-scaled and use the per-item cook requirement as the duration baseline.
func (s *Session) ViewFurnaceUpdate(prevCookTime, cookTime, prevRemainingFuelTime, remainingFuelTime, prevMaxFuelTime, maxFuelTime time.Duration) {
	if prevCookTime != cookTime {
		s.writePacket(&mc.ContainerSetDataPacket{
			WindowID: byte(s.openedWindowID.Load()),
			Key:      mc.ContainerDataFurnaceTickCount,
			Value:    int32(cookTime.Milliseconds() / 50),
		})
	}

	if prevRemainingFuelTime != remainingFuelTime {
		s.writePacket(&mc.ContainerSetDataPacket{
			WindowID: byte(s.openedWindowID.Load()),
			Key:      mc.ContainerDataFurnaceLitTime,
			Value:    int32(remainingFuelTime.Milliseconds() / 50),
		})
	}

	if prevMaxFuelTime != maxFuelTime {
		s.writePacket(&mc.ContainerSetDataPacket{
			WindowID: byte(s.openedWindowID.Load()),
			Key:      mc.ContainerDataFurnaceLitDuration,
			Value:    int32(maxFuelTime.Milliseconds() / 50),
		})
	}
}

// ViewBrewingUpdate updates a brewing stand for the associated session based on previous times.
func (s *Session) ViewBrewingUpdate(prevBrewTime, brewTime time.Duration, prevFuelAmount, fuelAmount, prevFuelTotal, fuelTotal int32) {
	if prevBrewTime != brewTime {
		s.writePacket(&mc.ContainerSetDataPacket{
			WindowID: byte(s.openedWindowID.Load()),
			Key:      mc.ContainerDataBrewingStandBrewTime,
			Value:    int32(brewTime.Milliseconds() / 50),
		})
	}

	if prevFuelAmount != fuelAmount {
		s.writePacket(&mc.ContainerSetDataPacket{
			WindowID: byte(s.openedWindowID.Load()),
			Key:      mc.ContainerDataBrewingStandFuelAmount,
			Value:    fuelAmount,
		})
	}

	if prevFuelTotal != fuelTotal {
		s.writePacket(&mc.ContainerSetDataPacket{
			WindowID: byte(s.openedWindowID.Load()),
			Key:      mc.ContainerDataBrewingStandFuelTotal,
			Value:    fuelTotal,
		})
	}
}

// OpenMenu shows the client m, the menu its player opened in tx, in a new
// window: ContainerOpen, or UpdateEquip for the inventory of a mount, then
// the items of the menu, if it holds any of its own, and the slot states of
// a crafter. A lectern opens a window the session keeps no state for, as
// the client sends no close for it.
func (s *Session) OpenMenu(m *menu.Menu, tx *world.Tx) {
	if s.closeWindow(false) {
		// The player closes its menu before it opens another. A client
		// told to open a window while it shows one would show both.
		s.logWindow("menu closed by the server", "why", "another menu opens")
	}
	// A menu replaces the screen of the inventory, which the client then
	// does not close by its own window.
	s.resetInventory("a menu opens")
	nextID := s.nextWindowID()
	if m.Kind() != menu.Lectern {
		s.menu.Store(m)
		s.windows.menuAt = s.windows.stamp()
	}
	containerType := windowType(m, tx)
	s.openedContainerID.Store(uint32(containerType))

	pos := m.Pos()
	switch m.Kind() {
	case menu.Entity:
		e, _ := m.Entity().Entity(tx)
		if m.ContainerKind() == entity.ContainerMount {
			// BDS opens the inventory of a mount with UpdateEquip, which names the
			// slots of its screen, and no ContainerOpen.
			s.writePacket(&mc.UpdateEquipPacket{
				WindowID:                nextID,
				WindowType:              containerType,
				EntityUniqueID:          s.handleUniqueID(m.Entity()),
				SerialisedInventoryData: mountEquipDataOf(e),
			})
		} else {
			s.writePacket(&mc.ContainerOpenPacket{
				WindowID:                nextID,
				ContainerType:           containerType,
				ContainerPosition:       mc.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])},
				ContainerEntityUniqueID: s.handleUniqueID(m.Entity()),
			})
		}
		s.sendInv(m.Inventory(), uint32(nextID))
	case menu.Merchant:
		s.writePacket(&mc.ContainerOpenPacket{
			WindowID:                nextID,
			ContainerType:           containerType,
			ContainerPosition:       mc.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])},
			ContainerEntityUniqueID: s.handleUniqueID(m.Entity()),
		})
	default:
		s.writePacket(&mc.ContainerOpenPacket{
			WindowID:                nextID,
			ContainerType:           containerType,
			ContainerPosition:       mc.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])},
			ContainerEntityUniqueID: -1,
		})
		if blockContainer(m.Kind()) || m.Kind() == menu.EnderChest {
			s.sendInv(m.Inventory(), uint32(nextID))
		}
		if crafter, ok := tx.Block(pos).(block.Crafter); ok && m.Kind() == menu.Crafter {
			for slot := range 9 {
				s.ViewCrafterSlotState(slot, crafter.DisabledSlots&(1<<slot) != 0)
			}
			s.ViewCrafterTriggered(crafter.Triggered)
		}
	}
}

// blockContainer reports whether menus of the kind passed are open for a
// block.Container.
func blockContainer(k menu.Kind) bool {
	switch k {
	case menu.Generic, menu.Furnace, menu.BlastFurnace, menu.Smoker, menu.BrewingStand, menu.Hopper, menu.Crafter:
		return true
	}
	return false
}

// window returns the inventory of the menu the player has open as requests
// name it: that of a block container, of an entity or the ender chest of
// the player, and noWindow while no menu, or one with no items of its own,
// is open.
func (s *Session) window() *inventory.Inventory {
	if m := s.menu.Load(); m != nil && (blockContainer(m.Kind()) || m.Kind() == menu.EnderChest || m.Kind() == menu.Entity) {
		return m.Inventory()
	}
	return s.noWindow
}

// windowType returns the type of the window that m opens in, as BDS
// 1.26.52.3 opens it.
func windowType(m *menu.Menu, tx *world.Tx) byte {
	switch m.Kind() {
	case menu.Generic:
		switch tx.Block(m.Pos()).(type) {
		case block.Dispenser:
			// BDS 1.26.52.3 opens a dispenser as type 6 and a dropper as 7
			// (testdata/bedrock_1_26_52_menu_requests.json).
			return mc.ContainerTypeDispenser
		case block.Dropper:
			return mc.ContainerTypeDropper
		}
	case menu.Furnace:
		return mc.ContainerTypeFurnace
	case menu.BrewingStand:
		return mc.ContainerTypeBrewingStand
	case menu.BlastFurnace:
		return mc.ContainerTypeBlastFurnace
	case menu.Smoker:
		return mc.ContainerTypeSmoker
	case menu.Hopper:
		return mc.ContainerTypeHopper
	case menu.Crafter:
		return mc.ContainerTypeCrafter
	case menu.Crafting:
		return mc.ContainerTypeWorkbench
	case menu.Enchantment:
		return mc.ContainerTypeEnchantment
	case menu.Anvil:
		return mc.ContainerTypeAnvil
	case menu.Beacon:
		return mc.ContainerTypeBeacon
	case menu.Lectern:
		return mc.ContainerTypeLectern
	case menu.Loom:
		return mc.ContainerTypeLoom
	case menu.Grindstone:
		return mc.ContainerTypeGrindstone
	case menu.Cartography:
		return mc.ContainerTypeCartography
	case menu.Stonecutter:
		return mc.ContainerTypeStonecutter
	case menu.Smithing:
		return mc.ContainerTypeSmithingTable
	case menu.Entity:
		t, _ := entityContainerType(m.ContainerKind())
		return t
	case menu.Merchant:
		return mc.ContainerTypeTrade
	}
	// A chest, a barrel, an ender chest and any other block.
	return mc.ContainerTypeContainer
}

// CloseMenu closes the window of the menu m: the client closed it itself if
// clientRequested, otherwise it is sent a close.
func (s *Session) CloseMenu(_ *menu.Menu, clientRequested bool) {
	s.closeWindow(clientRequested)
}

// entityContainerType returns the container type that the inventory of an
// entity of the kind passed opens with, as BDS 1.26.52.3 opens it: a mount
// with UpdateEquip of window type 12 (Horse), for every mount, chested or
// not; a chest boat and a chest minecart as a plain container (0); a hopper
// minecart as a hopper (8). The actor data of these entities names other
// types, which they give themselves.
func entityContainerType(kind entity.ContainerKind) (byte, bool) {
	switch kind {
	case entity.ContainerMount:
		return mc.ContainerTypeHorse, true
	case entity.ContainerChestBoat, entity.ContainerMinecartChest:
		return mc.ContainerTypeContainer, true
	case entity.ContainerMinecartHopper:
		return mc.ContainerTypeHopper, true
	}
	return 0, false
}

// ViewTrades shows the client the offers of merchant, the merchant of the
// menu m, in the window of m.
func (s *Session) ViewTrades(m *menu.Menu, merchant entity.Merchant, offers []entity.MerchantOffer) {
	serializedOffers, err := serializeMerchantOffers(offers)
	if err != nil {
		if s.conf.Log != nil {
			s.conf.Log.Error("failed to serialize merchant offers", "err", err)
		}
		return
	}
	s.sendMerchantOffers(m.Entity(), merchant, s.ent, offers, serializedOffers)
}

func (s *Session) sendMerchantOffers(e *world.EntityHandle, merchant entity.Merchant, user *world.EntityHandle, offers []entity.MerchantOffer, serializedOffers []byte) {
	level := merchant.MerchantLevel() - 1
	if level < 0 {
		level = 0
	}
	s.writePacket(&mc.UpdateTradePacket{
		WindowID:          byte(s.openedWindowID.Load()),
		WindowType:        15,
		Size:              int32(len(offers)),
		TradeTier:         int32(level),
		VillagerUniqueID:  s.handleUniqueID(e),
		EntityUniqueID:    s.handleUniqueID(user),
		DisplayName:       merchant.MerchantName(),
		NewTradeUI:        true,
		DemandBasedPrices: true,
		SerialisedOffers:  serializedOffers,
	})
}

func serializeMerchantOffers(offers []entity.MerchantOffer) ([]byte, error) {
	recipes := make([]any, len(offers))
	for i, offer := range offers {
		rewardXP := byte(0)
		if offer.RewardXP {
			rewardXP = 1
		}
		// The client shows buyA as priced and names the offer by netId, the
		// recipe network ID its trade request carries.
		recipes[i] = map[string]any{
			"netId":            int32(i + 1),
			"buyA":             tradeItemNBT(offer.CostA()),
			"buyB":             tradeItemNBT(offer.SecondaryInput),
			"buyCountA":        int32(offer.Input.Count()),
			"buyCountB":        int32(offer.SecondaryInput.Count()),
			"sell":             tradeItemNBT(offer.Output),
			"uses":             int32(offer.Uses),
			"maxUses":          int32(offer.MaxUses),
			"rewardExp":        rewardXP,
			"tier":             int32(offer.Tier),
			"traderExp":        int32(offer.XP),
			"priceMultiplierA": offer.PriceMultiplier,
			"priceMultiplierB": float32(0),
			"demand":           int32(offer.Demand),
		}
	}
	// The experience a villager needs for each level, as
	// VillagerData.NEXT_LEVEL_XP_THRESHOLDS.
	tiers := []any{
		map[string]any{"0": int32(0)}, map[string]any{"1": int32(10)}, map[string]any{"2": int32(70)},
		map[string]any{"3": int32(150)}, map[string]any{"4": int32(250)},
	}
	var buf bytes.Buffer
	if err := nbt.NewEncoderWithEncoding(&buf, nbt.NetworkLittleEndian).Encode(map[string]any{"Recipes": recipes, "TierExpRequirements": tiers}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ViewSlotChange ...
func (s *Session) ViewSlotChange(slot int, newItem item.Stack) {
	if s.menu.Load() == nil {
		return
	}
	if s.inTransaction.Load() {
		// Don't send slot changes to the player itself.
		return
	}
	s.writePacket(&mc.InventorySlotPacket{
		WindowID: s.openedWindowID.Load(),
		Slot:     uint32(slot),
		NewItem:  instanceFromItem(s.tables(), newItem),
	})
}

// ViewCrafterSlotState sends the disabled/enabled state for one native Crafter slot.
func (s *Session) ViewCrafterSlotState(slot int, disabled bool) {
	if s.menu.Load() == nil || s.openedContainerID.Load() != uint32(mc.ContainerTypeCrafter) {
		return
	}
	value := int32(0)
	if disabled {
		value = 1
	}
	s.writePacket(&mc.ContainerSetDataPacket{WindowID: byte(s.openedWindowID.Load()), Key: int32(slot), Value: value})
}

// ViewCrafterTriggered sends the native Crafter's redstone data property.
func (s *Session) ViewCrafterTriggered(triggered bool) {
	if s.menu.Load() == nil || s.openedContainerID.Load() != uint32(mc.ContainerTypeCrafter) {
		return
	}
	value := int32(0)
	if triggered {
		value = 1
	}
	s.writePacket(&mc.ContainerSetDataPacket{WindowID: byte(s.openedWindowID.Load()), Key: 9, Value: value})
}

// nextWindowID produces the next window ID for a new window. It is an int of 1-99.
func (s *Session) nextWindowID() byte {
	if s.openedWindowID.CompareAndSwap(99, 1) {
		return 1
	}
	return byte(s.openedWindowID.Add(1))
}

// closeWindow closes the container window currently opened. If no window is open, closeWindow will do
// nothing.
func (s *Session) closeWindow(clientRequested bool) bool {
	if s.menu.Swap(nil) == nil {
		return false
	}
	containerType := byte(s.openedContainerID.Load())
	windowID := byte(s.openedWindowID.Load())
	s.openedContainerID.Store(0)
	if !clientRequested {
		s.writePacket(&mc.ContainerClosePacket{
			WindowID:      windowID,
			ContainerType: containerType,
			ServerSide:    true,
		})
	}
	return true
}
