package session_test

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// stonecutterRequestsScenario cuts stone at the stonecutter as
// TestMenuRequestScenarios runs it: one cut, two at once, slabs of more
// than a stack that the client creates one stack at a time, and the cuts
// the stonecutter refuses, some after actions of the same request that the
// refusal undoes: an unknown recipe, a shaped recipe and a shapeless one of
// the crafting table, no cuts, too little stone, the wrong input, a locked
// input, and the stonecutter replaced under the open menu.
func (s *scenario) stonecutterRequestsScenario() {
	const inv = v2193.ContainerCombinedHotBarAndInventory
	input := func() v2193.StackRequestSlotInfo {
		return s.slot(v2193.ContainerStonecutterInput, uiStonecutterInput)
	}
	bricks := s.recipe("stonecutter", block.StoneBricks{}, "minecraft:stone\"")
	slabs := s.recipe("stonecutter", block.Slab{Block: block.Stone{}}, "minecraft:stone\"")
	sticks := s.recipe("crafting_table", item.Stick{}, "planks")
	redDye := s.recipe("crafting_table", item.Dye{Colour: item.ColourRed()}, "poppy")
	cut := func(heading string, recipe uint32, times, count, to int, accept bool) {
		s.t.Helper()
		actions := []v2193.StackRequestAction{
			&v2193.CraftRecipeStackRequestAction{RecipeNetworkID: recipe, NumberOfCrafts: uint8(times)},
			&v2193.ConsumeStackRequestAction{Count: uint8(max(times, 1)), Source: input()},
			&v2193.PlaceStackRequestAction{Count: uint8(count), Source: s.created(), Destination: s.slot(inv, to)},
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
	s.give("stone and granite given",
		slotStack{0, item.NewStack(block.Stone{}, 64)},
		slotStack{1, item.NewStack(block.Granite{}, 4)},
	)
	stonecutter := cube.Pos{0, -60, -2}
	s.place("a stonecutter is placed", stonecutter, block.Stonecutter{})
	s.clickBlock("the client opens the stonecutter", stonecutter)
	s.request("four stone put in it", nil,
		&v2193.PlaceStackRequestAction{Count: 4, Source: s.slot(inv, 0), Destination: input()})

	cut("stone bricks cut", bricks, 1, 1, 10, true)
	cut("two stone bricks cut at once", bricks, 2, 2, 10, true)
	cut("refused: an unknown recipe", 0xfffe, 1, 1, 11, false)
	cut("refused: sticks, shaped, of stone", sticks, 1, 4, 11, false)
	cut("refused: red dye, of the crafting table", redDye, 1, 1, 11, false)
	cut("refused: no cuts", bricks, 0, 1, 11, false)
	cut("refused: two cuts of one stone", bricks, 2, 2, 11, false)

	// More stone put in first: the refusal takes it out again.
	s.refuse("refused: stone put in, then three bricks of it",
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 0), Destination: input()},
		&v2193.CraftRecipeStackRequestAction{RecipeNetworkID: bricks, NumberOfCrafts: 3},
		&v2193.ConsumeStackRequestAction{Count: 3, Source: s.again(v2193.ContainerStonecutterInput, uiStonecutterInput)})
	// The stone swapped for granite first: the wrong input.
	s.refuse("refused: the stone swapped for granite, then bricks",
		&v2193.SwapStackRequestAction{Source: input(), Destination: s.slot(inv, 1)},
		&v2193.CraftRecipeStackRequestAction{RecipeNetworkID: bricks, NumberOfCrafts: 1},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.again(v2193.ContainerStonecutterInput, uiStonecutterInput)})

	// 33 cuts make 66 slabs, two stacks, which the client creates one by
	// one.
	s.request("33 stone put in it", nil,
		&v2193.PlaceStackRequestAction{Count: 33, Source: s.slot(inv, 0), Destination: input()})
	s.request("66 slabs cut: two results, created one by one", nil,
		&v2193.CraftRecipeStackRequestAction{RecipeNetworkID: slabs, NumberOfCrafts: 33},
		&v2193.ConsumeStackRequestAction{Count: 33, Source: input()},
		&v2193.CreateStackRequestAction{ResultsSlot: 0},
		&v2193.PlaceStackRequestAction{Count: 64, Source: s.created(), Destination: s.slot(inv, 12)},
		&v2193.CreateStackRequestAction{ResultsSlot: 1},
		&v2193.PlaceStackRequestAction{Count: 2, Source: s.created(), Destination: s.slot(inv, 13)})
	s.refuse("refused: four slabs cut, their one result created again",
		&v2193.PlaceStackRequestAction{Count: 2, Source: s.slot(inv, 0), Destination: input()},
		&v2193.CraftRecipeStackRequestAction{RecipeNetworkID: slabs, NumberOfCrafts: 2},
		&v2193.ConsumeStackRequestAction{Count: 2, Source: s.again(v2193.ContainerStonecutterInput, uiStonecutterInput)},
		&v2193.CreateStackRequestAction{ResultsSlot: 0},
		&v2193.CreateStackRequestAction{ResultsSlot: 0})

	s.step("a locked stone in the stonecutter", func(_ *world.Tx, _ *player.Player) {
		_, _, _, ui, _ := s.sess.Inventories()
		if err := ui.SetItem(uiStonecutterInput, item.NewStack(block.Stone{}, 1).WithLockMode(item.LockInInventory)); err != nil {
			s.t.Fatal(err)
		}
	})
	cut("refused: bricks of a locked stone", bricks, 1, 1, 14, false)
	s.step("the locked stone taken away", func(_ *world.Tx, _ *player.Player) {
		_, _, _, ui, _ := s.sess.Inventories()
		_ = ui.SetItem(uiStonecutterInput, item.NewStack(block.Stone{}, 2))
	})

	// Without the stonecutter the request is a craft of the crafting grid.
	s.place("the stonecutter replaced by stone", stonecutter, block.Stone{})
	cut("refused: bricks with no stonecutter", bricks, 1, 1, 14, false)
	s.closeContainer("the client closes the stonecutter")
}
