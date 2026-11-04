package session_test

import (
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// TestGoldenRefusals records what a client is sent for item stack requests
// the server refuses, and for the requests that show what the refusal left
// behind: a request that changes a slot twice, one that changes the off
// hand, a loom craft with no menu open, and an item put in the container
// of a menu that has none. It also opens a dispenser and a dropper. The
// requests are those of the BDS capture in
// testdata/bedrock_1_26_52_menu_requests.json, which BDS answered as the
// comments say, and a refused request that changed two slots of the
// crafting grid.
func TestGoldenRefusals(t *testing.T) {
	if isolated(t) {
		return
	}
	s := newScenario(t, "refusals", scenarioConfig{})
	s.refusalsScenario()
	s.snapshot()
}

// refusalsScenario runs the steps of TestGoldenRefusals.
func (s *scenario) refusalsScenario() {
	const inv = mc.ContainerCombinedHotBarAndInventory

	s.step("items given", func(_ *world.Tx, p *player.Player) {
		inv := p.Inventory()
		for _, it := range []struct {
			slot  int
			stack item.Stack
		}{
			{0, item.NewStack(item.Apple{}, 16)},
			{2, item.NewStack(block.Planks{Wood: block.OakWood()}, 8)},
			{3, item.NewStack(item.Shield{}, 1)},
			{4, item.NewStack(item.Totem{}, 1)},
			{5, item.NewStack(block.Banner{Colour: item.ColourWhite()}, 1)},
			{6, item.NewStack(item.Dye{Colour: item.ColourRed()}, 1)},
		} {
			_ = inv.SetItem(it.slot, it.stack)
		}
	})
	s.step("the empty slot held", func(_ *world.Tx, p *player.Player) {
		if err := p.SetHeldSlot(emptyHand); err != nil {
			s.t.Error(err)
		}
	})

	// A plank goes to slot 20 and another to slot 21, both from slot 2,
	// before an action names the apples by a stack ID they do not have. BDS
	// refuses it (49) and then gives all eight planks of slot 2 to the
	// cursor.
	stale := mc.StackRequestSlotInfo{Container: mc.FullContainerName{ContainerID: inv}, Slot: 0, StackNetworkID: 9999}
	s.refuse("refused: slot 2 changed twice, then a stale stack ID",
		&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 2), Destination: s.slot(inv, 20)},
		&mc.PlaceStackRequestAction{Count: 1, Source: s.again(inv, 2), Destination: s.slot(inv, 21)},
		&mc.PlaceStackRequestAction{Count: 1, Source: stale, Destination: s.slot(inv, 22)})
	s.attempt("the eight planks of slot 2 taken to the cursor",
		&mc.TakeStackRequestAction{Count: 8, Source: s.slot(inv, 2), Destination: s.slot(mc.ContainerCursor, uiCursor)})
	s.attempt("the planks of the cursor put back",
		&mc.PlaceStackRequestAction{Count: 8, Source: s.slot(mc.ContainerCursor, uiCursor), Destination: s.slot(inv, 2)})

	// The client names the off hand as slot 1, the only slot BDS accepts.
	// BDS refuses the request (49) and then takes a totem in the off hand.
	s.refuse("refused: a shield put in the off hand, then a stale stack ID",
		&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 3), Destination: s.slot(mc.ContainerOffhand, 1)},
		&mc.PlaceStackRequestAction{Count: 1, Source: stale, Destination: s.slot(inv, 22)})
	s.attempt("a totem put in the off hand",
		&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 4), Destination: s.slot(mc.ContainerOffhand, 1)})

	// No menu was opened yet. BDS refuses the craft (7).
	s.refuse("refused: a loom craft with no menu open",
		&mc.CraftLoomRecipeStackRequestAction{Pattern: "bo", TimesCrafted: 1},
		&mc.ConsumeStackRequestAction{Count: 1, Source: s.slot(mc.ContainerLoomInput, uiLoomInput)},
		&mc.ConsumeStackRequestAction{Count: 1, Source: s.slot(mc.ContainerLoomDye, uiLoomDye)},
		&mc.PlaceStackRequestAction{Count: 1, Source: s.again(mc.ContainerCreatedOutput, uiCreatedOutput), Destination: s.slot(inv, 7)})

	// An anvil and a crafting table hold no items of their own: BDS refuses
	// an item put in container 7 (50), at slot 0 and at slot 1.
	for _, menu := range []struct {
		name string
		pos  cube.Pos
		b    world.Block
	}{
		{"an anvil", cube.Pos{-2, -60, 0}, block.Anvil{Type: block.UndamagedAnvil()}},
		{"a crafting table", cube.Pos{-2, -60, 2}, block.CraftingTable{}},
	} {
		s.place(menu.name+" is placed", menu.pos, menu.b)
		s.clickBlock("the client opens "+menu.name, menu.pos)
		for _, slot := range []int{0, 1} {
			s.attempt("an apple put in the container of "+menu.name+fmt.Sprintf(", slot %d", slot),
				&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 0), Destination: s.slot(mc.ContainerLevelEntity, slot)})
		}
		if _, ok := menu.b.(block.CraftingTable); ok {
			// Each write of the UI sends the whole of it: the undo sends the
			// grid as it was before the second plank, then before the first.
			s.refuse("refused: two planks put in the grid, then a stale stack ID",
				&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 2), Destination: s.slot(mc.ContainerCraftingInput, uiCraftingGrid)},
				&mc.PlaceStackRequestAction{Count: 1, Source: s.again(inv, 2), Destination: s.slot(mc.ContainerCraftingInput, uiCraftingGrid+3)},
				&mc.PlaceStackRequestAction{Count: 1, Source: stale, Destination: s.slot(inv, 22)})
		}
		s.closeContainer("the client closes " + menu.name)
	}

	// A dispenser and a dropper, with three iron ingots in slot 4. BDS opens
	// them as types 6 and 7 and takes an apple in slot 0.
	for _, menu := range []struct {
		name string
		pos  cube.Pos
		b    world.Block
	}{
		{"a dispenser", cube.Pos{2, -60, 0}, block.NewDispenser()},
		{"a dropper", cube.Pos{2, -60, 2}, block.NewDropper()},
	} {
		s.step(menu.name+" is placed with iron ingots in it", func(tx *world.Tx, _ *player.Player) {
			tx.SetBlock(menu.pos, menu.b, nil)
			c, ok := tx.Block(menu.pos).(block.Container)
			if !ok {
				s.t.Fatalf("%s holds no inventory", menu.name)
			}
			if err := c.Inventory(tx, menu.pos).SetItem(4, item.NewStack(item.IronIngot{}, 3)); err != nil {
				s.t.Fatal(err)
			}
		})
		s.clickBlock("the client opens "+menu.name, menu.pos)
		s.request("an apple put in "+menu.name, nil,
			&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 0), Destination: s.slot(mc.ContainerLevelEntity, 0)})
		s.closeContainer("the client closes " + menu.name)
	}
}

// refuse sends an item stack request of the actions passed and fails the
// test unless the server refused it.
func (s *scenario) refuse(heading string, actions ...mc.StackRequestAction) {
	s.t.Helper()
	if status, ok := s.attempt(heading, actions...); ok && status == mc.ItemStackResponseStatusOK {
		s.t.Errorf("%s: the request was accepted", heading)
	}
}

// refuseWith sends an item stack request of the actions passed and fails
// the test unless the server refused it with the status passed, the status
// BDS 1.26.52.3 refuses the request with.
func (s *scenario) refuseWith(heading string, status uint8, actions ...mc.StackRequestAction) {
	s.t.Helper()
	if got, ok := s.attempt(heading, actions...); ok && got != status {
		s.t.Errorf("%s: answered with status %d, want %d", heading, got, status)
	}
}

// attempt sends an item stack request of the actions passed and returns
// the status the server answered with, whatever it is.
func (s *scenario) attempt(heading string, actions ...mc.StackRequestAction) (uint8, bool) {
	s.t.Helper()
	id := s.nextRequest
	s.nextRequest -= 2
	before := len(s.responses)
	s.client(heading, &mc.ItemStackRequestPacket{Requests: []mc.ItemStackRequest{{RequestID: id, Actions: actions}}})
	if len(s.responses) == before {
		s.t.Errorf("%s: the request was not answered", heading)
		return 0, false
	}
	return s.responses[len(s.responses)-1], true
}
