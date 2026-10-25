package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestVerifySlotResyncsStaleCount(t *testing.T) {
	s := testSession("stale-slot")
	s.br = world.DefaultBlockRegistry
	s.inv = inventory.New(36, nil)
	actual := item.NewStack(item.Beef{}, 5)
	if err := s.inv.SetItem(0, actual); err != nil {
		t.Fatal(err)
	}
	if err := s.VerifySlot(0, actual.Grow(-1)); err != nil {
		t.Fatalf("stale client count rejected: %v", err)
	}
	pk, ok := (<-s.packets).(*packet.InventorySlot)
	if !ok || pk.WindowID != protocol.WindowIDInventory || pk.Slot != 0 || pk.NewItem.Stack.Count != 5 {
		t.Fatalf("slot correction = %#v, want five beef in inventory slot zero", pk)
	}
	if got, _ := s.inv.Item(0); !got.Equal(actual) {
		t.Errorf("verification changed the authoritative stack: %v", got)
	}
}
