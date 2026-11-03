package session_test

import (
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// TestGoldenRefusals records what a client is sent for item stack requests
// the server refuses, and for the requests that show what the refusal left
// behind: a request that changes a slot twice, one that changes the off
// hand, a loom craft with no menu open, and an item put in the container
// of a menu that has none. It also opens a dispenser and a dropper. The
// requests are those of the BDS capture in
// testdata/bedrock_1_26_52_menu_requests.json, which BDS answered as the
// comments say.
func TestGoldenRefusals(t *testing.T) {
	if isolated(t) {
		return
	}
	s := newScenario(t, "refusals", scenarioConfig{})
	const inv = v2193.ContainerCombinedHotBarAndInventory

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
	stale := v2193.StackRequestSlotInfo{Container: v2193.FullContainerName{ContainerID: inv}, Slot: 0, StackNetworkID: 9999}
	s.refuse("refused: slot 2 changed twice, then a stale stack ID",
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 2), Destination: s.slot(inv, 20)},
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.again(inv, 2), Destination: s.slot(inv, 21)},
		&v2193.PlaceStackRequestAction{Count: 1, Source: stale, Destination: s.slot(inv, 22)})
	s.attempt("the eight planks of slot 2 taken to the cursor",
		&v2193.TakeStackRequestAction{Count: 8, Source: s.slot(inv, 2), Destination: s.slot(v2193.ContainerCursor, uiCursor)})
	s.attempt("the planks of the cursor put back",
		&v2193.PlaceStackRequestAction{Count: 8, Source: s.slot(v2193.ContainerCursor, uiCursor), Destination: s.slot(inv, 2)})

	// The client names the off hand as slot 1, the only slot BDS accepts.
	// BDS refuses the request (49) and then takes a totem in the off hand.
	s.refuse("refused: a shield put in the off hand, then a stale stack ID",
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 3), Destination: s.slot(v2193.ContainerOffhand, 1)},
		&v2193.PlaceStackRequestAction{Count: 1, Source: stale, Destination: s.slot(inv, 22)})
	s.attempt("a totem put in the off hand",
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 4), Destination: s.slot(v2193.ContainerOffhand, 1)})

	// No menu was opened yet. BDS refuses the craft (7).
	s.refuse("refused: a loom craft with no menu open",
		&v2193.CraftLoomRecipeStackRequestAction{Pattern: "bo", TimesCrafted: 1},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerLoomInput, uiLoomInput)},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerLoomDye, uiLoomDye)},
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.again(v2193.ContainerCreatedOutput, uiCreatedOutput), Destination: s.slot(inv, 7)})

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
				&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 0), Destination: s.slot(v2193.ContainerLevelEntity, slot)})
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
			&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 0), Destination: s.slot(v2193.ContainerLevelEntity, 0)})
		s.closeContainer("the client closes " + menu.name)
	}
	s.snapshot()
}

// refuse sends an item stack request of the actions passed and fails the
// test unless the server refused it.
func (s *scenario) refuse(heading string, actions ...v2193.StackRequestAction) {
	s.t.Helper()
	if status, ok := s.attempt(heading, actions...); ok && status == v2193.ItemStackResponseStatusOK {
		s.t.Errorf("%s: the request was accepted", heading)
	}
}

// attempt sends an item stack request of the actions passed and returns
// the status the server answered with, whatever it is.
func (s *scenario) attempt(heading string, actions ...v2193.StackRequestAction) (uint8, bool) {
	s.t.Helper()
	id := s.nextRequest
	s.nextRequest -= 2
	before := len(s.responses)
	s.client(heading, &v2193.ItemStackRequestPacket{Requests: []v2193.ItemStackRequest{{RequestID: id, Actions: actions}}})
	if len(s.responses) == before {
		s.t.Errorf("%s: the request was not answered", heading)
		return 0, false
	}
	return s.responses[len(s.responses)-1], true
}
