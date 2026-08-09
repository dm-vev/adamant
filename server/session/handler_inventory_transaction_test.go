package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestUseItemTransactionResyncsHeldSlotAfterAction(t *testing.T) {
	for _, test := range []struct {
		name      string
		count     int
		useItem   func(*Session)
		wantCount uint16
	}{
		{name: "cancelled", count: 1, useItem: func(s *Session) { s.writePacket(&packet.Text{Message: "cancelled use-item"}) }, wantCount: 1},
		{name: "successful", count: 2, useItem: func(s *Session) {
			s.writePacket(&packet.Text{Message: "successful use-item"})
			_ = s.inv.SetItem(0, item.NewStack(item.Apple{}, 1))
		}, wantCount: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := transactionTestSession(test.count)
			c := transactionControllable{useItem: func() { test.useItem(s) }}
			if err := new(InventoryTransactionHandler).handleUseItemTransaction(&protocol.UseItemTransactionData{ActionType: protocol.UseItemActionClickAir}, s, c); err != nil {
				t.Fatal(err)
			}
			assertActionThenHeldSlot(t, s, test.wantCount)
		})
	}
}

func TestUseItemOnEntityTransactionResyncsHeldSlotAfterAction(t *testing.T) {
	s := transactionTestSession(1)
	w := world.New()
	t.Cleanup(func() { _ = w.Close() })
	target := entity.NewText("target", [3]float64{})

	if err := w.Do(func(tx *world.Tx) {
		tx.AddEntity(target)
		s.entities[2] = target
		c := transactionControllable{useItemOnEntity: func(world.Entity) bool {
			s.writePacket(&packet.Text{Message: "cancelled use-on-entity"})
			return false
		}}
		err := new(InventoryTransactionHandler).handleUseItemOnEntityTransaction(&protocol.UseItemOnEntityTransactionData{
			TargetEntityRuntimeID: 2,
			ActionType:            protocol.UseItemOnEntityActionInteract,
		}, s, tx, c)
		if err != nil {
			t.Fatal(err)
		}
	}).Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertActionThenHeldSlot(t, s, 1)
}

func transactionTestSession(count int) *Session {
	s := testSession("transaction")
	s.br = world.DefaultBlockRegistry
	s.inv = inventory.New(36, nil)
	_ = s.inv.SetItem(0, item.NewStack(item.Apple{}, count))
	s.heldSlot.Store(new(uint32))
	return s
}

func assertActionThenHeldSlot(t *testing.T, s *Session, wantCount uint16) {
	t.Helper()
	if _, ok := (<-s.packets).(*packet.Text); !ok {
		t.Fatal("action packet was not sent before the inventory resync")
	}
	slot, ok := (<-s.packets).(*packet.InventorySlot)
	if !ok {
		t.Fatal("held slot was not resynced")
	}
	if slot.WindowID != protocol.WindowIDInventory || slot.Slot != 0 || slot.NewItem.Stack.Count != wantCount {
		t.Fatalf("held slot = %+v, want inventory slot 0 with count %d", slot, wantCount)
	}
	if len(s.packets) != 0 {
		t.Fatal("unexpected full inventory resend")
	}
}

type transactionControllable struct {
	Controllable
	useItem         func()
	useItemOnEntity func(world.Entity) bool
}

func (c transactionControllable) UseItem() { c.useItem() }

func (c transactionControllable) UseItemOnEntity(e world.Entity) bool { return c.useItemOnEntity(e) }
