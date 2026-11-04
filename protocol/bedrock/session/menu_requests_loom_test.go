package session_test

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// uiLoomPattern is the UI slot of the pattern item of the loom.
const uiLoomPattern = 11

// loomRequestsScenario weaves banners at the loom as
// TestMenuRequestScenarios runs them: patterns without and with a pattern item, two at once, and
// the crafts the loom refuses, some after actions of the same request that
// the refusal undoes: an unknown pattern, the wrong or no pattern item, an
// illager banner, no dye, too few banners, no crafts, locked inputs, and
// the loom broken under the open menu.
func (s *scenario) loomRequestsScenario() {
	const inv = mc.ContainerCombinedHotBarAndInventory
	loomInput := func() mc.StackRequestSlotInfo { return s.slot(mc.ContainerLoomInput, uiLoomInput) }
	loomDye := func() mc.StackRequestSlotInfo { return s.slot(mc.ContainerLoomDye, uiLoomDye) }
	loomPattern := func() mc.StackRequestSlotInfo {
		return s.slot(mc.ContainerLoomMaterial, uiLoomPattern)
	}
	weave := func(heading, pattern string, times int, to int, accept bool) {
		s.t.Helper()
		actions := []mc.StackRequestAction{
			&mc.CraftLoomRecipeStackRequestAction{Pattern: pattern, TimesCrafted: uint8(times)},
			&mc.ConsumeStackRequestAction{Count: uint8(max(times, 1)), Source: loomInput()},
			&mc.ConsumeStackRequestAction{Count: uint8(max(times, 1)), Source: loomDye()},
			&mc.PlaceStackRequestAction{Count: uint8(max(times, 1)), Source: s.created(), Destination: s.slot(inv, to)},
		}
		if accept {
			s.request(heading, nil, actions...)
		} else {
			s.refuse(heading, actions...)
		}
	}

	s.step("the empty slot held", func(_ *world.Tx, p *player.Player) {
		if err := p.SetHeldSlot(emptyHand); err != nil {
			s.t.Error(err)
		}
	})
	s.give("banners, dyes and pattern items given",
		slotStack{0, item.NewStack(block.Banner{Colour: item.ColourWhite()}, 6)},
		slotStack{1, item.NewStack(item.Dye{Colour: item.ColourRed()}, 6)},
		slotStack{2, item.NewStack(item.BannerPattern{Type: item.CreeperBannerPattern()}, 1)},
		slotStack{3, item.NewStack(block.Banner{Colour: item.ColourWhite(), Illager: true}, 1)},
		slotStack{4, item.NewStack(item.Apple{}, 4)},
	)
	loom := cube.Pos{-2, -60, 2}
	s.place("a loom is placed", loom, block.Loom{})
	s.clickBlock("the client opens the loom", loom)
	s.request("three banners and three dyes put in it", nil,
		&mc.PlaceStackRequestAction{Count: 3, Source: s.slot(inv, 0), Destination: loomInput()},
		&mc.PlaceStackRequestAction{Count: 3, Source: s.slot(inv, 1), Destination: loomDye()})

	weave("a border woven", "bo", 1, 10, true)
	weave("refused: an unknown pattern", "zzz", 1, 11, false)
	weave("refused: a creeper without its pattern item", "cre", 1, 11, false)
	weave("refused: no crafts", "bo", 0, 11, false)
	weave("refused: three borders of two banners", "bo", 3, 11, false)

	s.request("the creeper pattern item put in it", nil,
		&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 2), Destination: loomPattern()})
	weave("refused: a flower of the creeper pattern item", "flo", 1, 11, false)
	weave("a creeper woven, the pattern item kept", "cre", 1, 11, true)

	// The pattern item taken out first: the refusal puts it back.
	s.refuse("refused: the pattern item taken out, then a creeper",
		&mc.TakeStackRequestAction{Count: 1, Source: loomPattern(), Destination: s.slot(inv, 2)},
		&mc.CraftLoomRecipeStackRequestAction{Pattern: "cre", TimesCrafted: 1},
		&mc.ConsumeStackRequestAction{Count: 1, Source: loomInput()},
		&mc.ConsumeStackRequestAction{Count: 1, Source: loomDye()})
	// The dye swapped for an apple first: no dye.
	s.refuse("refused: the dye swapped for apples, then a border",
		&mc.SwapStackRequestAction{Source: loomDye(), Destination: s.slot(inv, 4)},
		&mc.CraftLoomRecipeStackRequestAction{Pattern: "bo", TimesCrafted: 1},
		&mc.ConsumeStackRequestAction{Count: 1, Source: loomInput()})
	// The banner swapped for the illager banner first.
	s.refuse("refused: the banner swapped for an illager banner, then a border",
		&mc.SwapStackRequestAction{Source: loomInput(), Destination: s.slot(inv, 3)},
		&mc.CraftLoomRecipeStackRequestAction{Pattern: "bo", TimesCrafted: 1},
		&mc.ConsumeStackRequestAction{Count: 1, Source: loomInput()})

	s.request("two more banners and dyes put in it", nil,
		&mc.PlaceStackRequestAction{Count: 2, Source: s.slot(inv, 0), Destination: loomInput()},
		&mc.PlaceStackRequestAction{Count: 2, Source: s.slot(inv, 1), Destination: loomDye()})
	s.request("the creeper pattern item taken out", nil,
		&mc.TakeStackRequestAction{Count: 1, Source: loomPattern(), Destination: s.slot(inv, 2)})
	weave("two borders woven at once", "bo", 2, 12, true)

	// Locked inputs, set as the server sets them.
	locked := func(heading string, slot int, stack item.Stack) {
		s.step(heading, func(_ *world.Tx, _ *player.Player) {
			_, _, _, ui, _ := s.sess.Inventories()
			if err := ui.SetItem(slot, stack); err != nil {
				s.t.Fatal(err)
			}
		})
	}
	locked("a locked banner in the loom", uiLoomInput, item.NewStack(block.Banner{Colour: item.ColourWhite()}, 1).WithLockMode(item.LockInInventory))
	weave("refused: a locked banner", "bo", 1, 13, false)
	locked("a banner in the loom", uiLoomInput, item.NewStack(block.Banner{Colour: item.ColourWhite()}, 1))
	locked("a locked dye in the loom", uiLoomDye, item.NewStack(item.Dye{Colour: item.ColourBlue()}, 1).WithLockMode(item.LockInSlot))
	weave("refused: a locked dye", "bo", 1, 13, false)
	locked("a dye in the loom", uiLoomDye, item.NewStack(item.Dye{Colour: item.ColourBlue()}, 1))
	locked("a locked pattern item in the loom", uiLoomPattern, item.NewStack(item.BannerPattern{Type: item.CreeperBannerPattern()}, 1).WithLockMode(item.LockInInventory))
	weave("refused: a locked pattern item", "bo", 1, 13, false)
	locked("the pattern item taken away", uiLoomPattern, item.Stack{})

	// Two requests in one packet: the second names the result of the first
	// by its request.
	first, second := s.nextRequest, s.nextRequest-2
	s.nextRequest -= 4
	s.client("a border woven and the banner moved, in one packet", &mc.ItemStackRequestPacket{Requests: []mc.ItemStackRequest{
		{RequestID: first, Actions: []mc.StackRequestAction{
			&mc.CraftLoomRecipeStackRequestAction{Pattern: "bo", TimesCrafted: 1},
			&mc.ConsumeStackRequestAction{Count: 1, Source: loomInput()},
			&mc.ConsumeStackRequestAction{Count: 1, Source: loomDye()},
			&mc.PlaceStackRequestAction{Count: 1, Source: mc.StackRequestSlotInfo{Container: mc.FullContainerName{ContainerID: mc.ContainerCreatedOutput}, Slot: uiCreatedOutput, StackNetworkID: first}, Destination: s.slot(inv, 13)},
		}},
		{RequestID: second, Actions: []mc.StackRequestAction{
			&mc.PlaceStackRequestAction{Count: 1, Source: mc.StackRequestSlotInfo{Container: mc.FullContainerName{ContainerID: inv}, Slot: 13, StackNetworkID: first}, Destination: s.slot(inv, 14)},
		}},
	}})

	// The loom broken under the open menu.
	s.request("a banner and a dye put in it again", nil,
		&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 0), Destination: loomInput()},
		&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 1), Destination: loomDye()})
	s.place("the loom replaced by stone", loom, block.Stone{})
	weave("refused: a border with no loom", "bo", 1, 15, false)
	s.closeContainer("the client closes the loom")
	weave("refused: a border with no menu", "bo", 1, 15, false)
}
