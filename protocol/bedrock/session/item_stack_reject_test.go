package session

import (
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// staleApples names inventory slot 0 by a stack ID it does not hold, which
// refuses the request it ends.
var staleApples = &v2193.PlaceStackRequestAction{Count: 1,
	Source:      v2193.StackRequestSlotInfo{Container: v2193.FullContainerName{ContainerID: v2193.ContainerCombinedHotBarAndInventory}, Slot: 0, StackNetworkID: 9999},
	Destination: v2193.StackRequestSlotInfo{Container: v2193.FullContainerName{ContainerID: v2193.ContainerCombinedHotBarAndInventory}, Slot: 22}}

func invSlot(container byte, slot int, id int32) v2193.StackRequestSlotInfo {
	return v2193.StackRequestSlotInfo{Container: v2193.FullContainerName{ContainerID: container}, Slot: byte(slot), StackNetworkID: id}
}

// TestRefusedRequestRestoresSlotChangedTwice refuses a request that took
// two planks from one slot, as BDS refused it in
// testdata/bedrock_1_26_52_menu_requests.json: the slot must hold all
// eight again, under each of the container IDs that name it.
func TestRefusedRequestRestoresSlotChangedTwice(t *testing.T) {
	for _, second := range []byte{v2193.ContainerCombinedHotBarAndInventory, v2193.ContainerHotBar} {
		s := craftingTestSession()
		planks := item.NewStack(block.Planks{Wood: block.OakWood()}, 8)
		_ = s.inv.SetItem(0, item.NewStack(item.Apple{}, 16))
		_ = s.inv.SetItem(2, planks)
		h := craftingTestHandler()
		err := h.handleRequest(v2193.ItemStackRequest{RequestID: -1, Actions: []v2193.StackRequestAction{
			&v2193.PlaceStackRequestAction{Count: 1, Source: invSlot(v2193.ContainerCombinedHotBarAndInventory, 2, item_id(planks)), Destination: invSlot(v2193.ContainerCombinedHotBarAndInventory, 20, 0)},
			&v2193.PlaceStackRequestAction{Count: 1, Source: invSlot(second, 2, -1), Destination: invSlot(v2193.ContainerCombinedHotBarAndInventory, 21, 0)},
			staleApples,
		}}, s, nil, merchantTestUser{mode: world.GameModeSurvival})
		if err == nil {
			t.Fatal("the request with a stale stack ID was accepted")
		}
		for slot, want := range map[int]int{2: 8, 20: 0, 21: 0} {
			if got, _ := s.inv.Item(slot); got.Count() != want || (want > 0 && item_id(got) != item_id(planks)) {
				t.Errorf("second take under container %d: slot %d holds %v after the refusal, want %d planks of the stack sent", second, slot, got, want)
			}
		}
	}
}

// TestRefusedRequestRestoresOffHand refuses a request that put a shield in
// the off hand, which the client names as slot 1, as BDS refused it: the
// off hand must be empty again and the shield back in its slot.
func TestRefusedRequestRestoresOffHand(t *testing.T) {
	s := craftingTestSession()
	shield := item.NewStack(item.Shield{}, 1)
	_ = s.inv.SetItem(0, item.NewStack(item.Apple{}, 16))
	_ = s.inv.SetItem(3, shield)
	h := craftingTestHandler()
	err := h.handleRequest(v2193.ItemStackRequest{RequestID: -1, Actions: []v2193.StackRequestAction{
		&v2193.PlaceStackRequestAction{Count: 1, Source: invSlot(v2193.ContainerCombinedHotBarAndInventory, 3, item_id(shield)), Destination: invSlot(v2193.ContainerOffhand, 1, 0)},
		staleApples,
	}}, s, nil, merchantTestUser{mode: world.GameModeSurvival})
	if err == nil {
		t.Fatal("the request with a stale stack ID was accepted")
	}
	if got, _ := s.offHand.Item(0); !got.Empty() {
		t.Errorf("the off hand holds %v after the refusal, want nothing", got)
	}
	if got, _ := s.inv.Item(3); got.Count() != 1 {
		t.Errorf("slot 3 holds %v after the refusal, want the shield", got)
	}
}

// TestRefusedRequestUndoesInReverse refuses a request that put a plank in
// grid slot 32, then one in 35: every write of the UI inventory sends the
// whole of it, so the order of the undo is what the client is sent. It
// must undo the last change first, every time.
func TestRefusedRequestUndoesInReverse(t *testing.T) {
	for range 50 {
		s := craftingTestSession()
		planks := item.NewStack(block.Planks{Wood: block.OakWood()}, 8)
		_ = s.inv.SetItem(0, item.NewStack(item.Apple{}, 16))
		_ = s.inv.SetItem(2, planks)
		var writes []int
		s.ui.SlotFunc(func(slot int, _, _ item.Stack) { writes = append(writes, slot) })
		h := craftingTestHandler()
		err := h.handleRequest(v2193.ItemStackRequest{RequestID: -1, Actions: []v2193.StackRequestAction{
			&v2193.PlaceStackRequestAction{Count: 1, Source: invSlot(v2193.ContainerCombinedHotBarAndInventory, 2, item_id(planks)), Destination: invSlot(v2193.ContainerCraftingInput, 28, 0)},
			&v2193.PlaceStackRequestAction{Count: 1, Source: invSlot(v2193.ContainerCombinedHotBarAndInventory, 2, -1), Destination: invSlot(v2193.ContainerCraftingInput, 29, 0)},
			staleApples,
		}}, s, nil, merchantTestUser{mode: world.GameModeSurvival})
		if err == nil {
			t.Fatal("the request with a stale stack ID was accepted")
		}
		if want := []int{28, 29, 29, 28}; !slices.Equal(writes, want) {
			t.Fatalf("the UI slots were written in the order %v, want %v", writes, want)
		}
	}
}

// TestLoomCraftWithNoMenuOpen refuses a loom craft when no menu is open,
// as BDS refused it (7), without reading the world: the position of the
// last menu opened says nothing once it closed.
func TestLoomCraftWithNoMenuOpen(t *testing.T) {
	s := craftingTestSession()
	h := craftingTestHandler()
	// No world is passed: reading a block would panic.
	err := h.handleRequest(v2193.ItemStackRequest{RequestID: -1, Actions: []v2193.StackRequestAction{
		&v2193.CraftLoomRecipeStackRequestAction{Pattern: "bo", TimesCrafted: 1},
	}}, s, nil, merchantTestUser{mode: world.GameModeSurvival})
	if err == nil {
		t.Fatal("the loom craft with no menu open was accepted")
	}
}
