package session

import (
	"fmt"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// InventoryTransactionHandler handles the InventoryTransaction packet.
type InventoryTransactionHandler struct{}

// Handle ...
func (h *InventoryTransactionHandler) Handle(p proto.Packet, s *Session, tx *world.Tx, c Controllable) (err error) {
	pk := p.(*mc.InventoryTransactionPacket)

	if len(pk.LegacySetItemSlots) > 2 {
		return fmt.Errorf("too many slot sync requests in inventory transaction")
	}

	defer func() {
		// The client has requested the server to resend the specified slots even if they haven't changed server-side.
		// Handling these requests is necessary to ensure the client's inventory remains in sync with the server.
		for _, slot := range pk.LegacySetItemSlots {
			if len(slot.Slots) > 2 {
				err = fmt.Errorf("too many slots in slot sync request")
				return
			}
			switch slot.ContainerID {
			case mc.ContainerOffhand:
				s.sendInv(s.offHand, mc.WindowIDOffHand)
			case mc.ContainerInventory:
				for _, slot := range slot.Slots {
					if i, err := s.inv.Item(int(slot)); err == nil {
						s.sendItem(i, int(slot), mc.WindowIDInventory)
					}
				}
			}
		}
	}()

	switch data := pk.TransactionData.(type) {
	case *mc.NormalTransactionData:
		h.resendInventories(s)
		// Always resend inventories with normal transactions. Most of the time we do not use these
		// transactions, so we're best off making sure the client and server stay in sync.
		if err := h.handleNormalTransaction(pk, s, c); err != nil {
			s.conf.Log.Debug("process packet: InventoryTransaction: verify Normal transaction actions: " + err.Error())
		}
		return
	case *mc.MismatchTransactionData:
		// Just resend the inventory and don't do anything.
		h.resendInventories(s)
		return
	case *mc.UseItemOnEntityTransactionData:
		if err = s.VerifyAndSetHeldSlot(int(data.HotBarSlot), stackToItem(s.tables(), data.HeldItem.Stack), c); err != nil {
			return
		}
		return h.handleUseItemOnEntityTransaction(data, s, tx, c)
	case *mc.UseItemTransactionData:
		if err = s.VerifyAndSetHeldSlot(int(data.HotBarSlot), stackToItem(s.tables(), data.HeldItem.Stack), c); err != nil {
			return
		}
		return h.handleUseItemTransaction(data, s, c)
	case *mc.ReleaseItemTransactionData:
		if err = s.VerifyAndSetHeldSlot(int(data.HotBarSlot), stackToItem(s.tables(), data.HeldItem.Stack), c); err != nil {
			return
		}
		return h.handleReleaseItemTransaction(c)
	}
	return fmt.Errorf("unhandled inventory transaction type %T", pk.TransactionData)
}

// resendInventories resends all inventories of the player.
func (h *InventoryTransactionHandler) resendInventories(s *Session) {
	s.sendInv(s.inv, mc.WindowIDInventory)
	s.sendInv(s.ui, mc.WindowIDUI)
	s.sendInv(s.offHand, mc.WindowIDOffHand)
	s.sendInv(s.armour.Inventory(), mc.WindowIDArmour)
}

// resendHeldSlot sends the authoritative stack after a client-predicted action.
func (h *InventoryTransactionHandler) resendHeldSlot(s *Session) error {
	heldSlot := s.heldSlot.Load()
	if heldSlot == nil {
		return fmt.Errorf("held slot not initialised for inventory transaction")
	}
	slot := int(atomic.LoadUint32(heldSlot))
	it, _ := s.inv.Item(slot)
	s.sendItem(it, slot, mc.WindowIDInventory)
	return nil
}

// handleNormalTransaction ...
func (h *InventoryTransactionHandler) handleNormalTransaction(pk *mc.InventoryTransactionPacket, s *Session, c Controllable) error {
	if len(pk.Actions) != 2 {
		return fmt.Errorf("expected two actions for dropping an item, got %d", len(pk.Actions))
	}

	var (
		slot     int
		count    int
		expected item.Stack
	)
	for _, action := range pk.Actions {
		windowID, hasWindowID := action.WindowID.Value()
		switch {
		case action.SourceType == mc.InventoryActionSourceWorld && action.InventorySlot == 0:
			if old := stackToItem(s.tables(), action.OldItem.Stack); !old.Empty() {
				return fmt.Errorf("unexpected non-empty old item in transaction action: %#v", action.OldItem)
			}
			count = int(action.NewItem.Stack.Count)
		case action.SourceType == mc.InventoryActionSourceContainer && hasWindowID && windowID == mc.WindowIDInventory:
			if expected = stackToItem(s.tables(), action.OldItem.Stack); expected.Empty() {
				return fmt.Errorf("unexpected empty old item in transaction action: %#v", action.OldItem)
			}
			slot = int(action.InventorySlot)
		default:
			return fmt.Errorf("unexpected action type in drop item transaction")
		}
	}

	actual, _ := s.inv.Item(slot)
	if count < 1 {
		return fmt.Errorf("expected at least one item to be dropped, got %d", count)
	}
	if count > actual.Count() {
		return fmt.Errorf("tried to throw %v items, but held only %v in slot", count, actual.Count())
	}
	if !expected.Equal(actual) {
		return fmt.Errorf("different item thrown than held in slot: %#v was thrown but held %#v", expected, actual)
	}
	// The player checks the count and the lock again, lets the handlers see
	// the drop and drops what is left of the stack in the slot.
	return c.DropFromInventory(slot, count)
}

// handleUseItemOnEntityTransaction ...
func (h *InventoryTransactionHandler) handleUseItemOnEntityTransaction(data *mc.UseItemOnEntityTransactionData, s *Session, tx *world.Tx, c Controllable) error {
	s.swingingArm.Store(true)
	defer s.swingingArm.Store(false)

	if data.TargetEntityRuntimeID == selfEntityRuntimeID {
		return fmt.Errorf("invalid entity interaction: players cannot interact with themselves")
	}

	handle, ok := s.entityFromRuntimeID(data.TargetEntityRuntimeID)
	if !ok {
		// In some cases, for example when a falling block entity solidifies, latency may allow attacking an entity that
		// no longer exists server side. This is expected, so we shouldn't kick the player.
		s.conf.Log.Debug("invalid entity interaction: no entity with runtime ID", "ID", data.TargetEntityRuntimeID)
		return nil
	}
	e, ok := handle.Entity(tx)
	if !ok {
		s.conf.Log.Debug("invalid entity interaction: entity is not in the same world (anymore)", "ID", data.TargetEntityRuntimeID)
		return nil
	}
	switch data.ActionType {
	case mc.UseItemOnEntityActionInteract:
		c.UseItemOnEntity(e)
	case mc.UseItemOnEntityActionAttack:
		c.AttackEntity(e)
	default:
		return fmt.Errorf("unhandled UseItemOnEntity ActionType %v", data.ActionType)
	}
	return h.resendHeldSlot(s)
}

// handleUseItemTransaction ...
func (h *InventoryTransactionHandler) handleUseItemTransaction(data *mc.UseItemTransactionData, s *Session, c Controllable) error {
	pos := cube.Pos{int(data.BlockPosition[0]), int(data.BlockPosition[1]), int(data.BlockPosition[2])}
	if data.ClientPrediction == mc.ClientPredictionSuccess || data.ActionType == mc.UseItemActionBreakBlock {
		// Suppress echoing the swing animation only when the client has already predicted it locally.
		s.swingingArm.Store(true)
		defer s.swingingArm.Store(false)
	}

	switch data.ActionType {
	case mc.UseItemActionBreakBlock:
		c.DestroyBlock(pos)
	case mc.UseItemActionClickBlock:
		face, err := parseBlockFace(data.BlockFace)
		if err != nil {
			return err
		}
		c.UseItemOnBlock(pos, face, vec32To64(data.ClickedPosition))
	case mc.UseItemActionClickAir:
		c.UseItem()
	default:
		return fmt.Errorf("unhandled UseItem ActionType %v", data.ActionType)
	}
	return h.resendHeldSlot(s)
}

// handleReleaseItemTransaction ...
func (h *InventoryTransactionHandler) handleReleaseItemTransaction(c Controllable) error {
	c.ReleaseItem()
	return nil
}
