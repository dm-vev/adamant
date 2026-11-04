package session_test

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// enchantingTable is where the cases of the enchanting table place it.
var enchantingTable = cube.Pos{-2, -60, 0}

// enchant returns the actions of a request that takes an option of the
// enchanting table, as the client sends them.
func (s *scenario) enchant(option uint32, lapis int) []v2193.StackRequestAction {
	return []v2193.StackRequestAction{
		&v2193.CraftRecipeStackRequestAction{RecipeNetworkID: option, NumberOfCrafts: 1},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerEnchantingInput, uiEnchantingInput)},
		&v2193.ConsumeStackRequestAction{Count: byte(lapis), Source: s.slot(v2193.ContainerEnchantingMaterial, uiEnchantingLapis)},
	}
}

// enchantingCases are requests of the enchanting table refused, for an
// input there is none of, that cannot be enchanted or is more than one, an
// option out of range, levels and lapis short of what the option needs,
// and a request whose earlier action the refusal undoes; then an option
// taken in survival, and requests once the table is gone from under the
// open menu.
func (s *scenario) enchantingCases() {
	const inv = v2193.ContainerCombinedHotBarAndInventory
	s.give("items given",
		slotStack{0, item.NewStack(item.Pickaxe{Tier: item.ToolTierIron}, 1)},
		slotStack{1, item.NewStack(item.Book{}, 2)},
		slotStack{2, item.NewStack(item.Stick{}, 1)},
		slotStack{3, item.NewStack(item.LapisLazuli{}, 5)},
	)
	s.step("the empty slot held", func(_ *world.Tx, p *player.Player) {
		if err := p.SetHeldSlot(emptyHand); err != nil {
			s.t.Error(err)
		}
	})
	s.place("an enchanting table is placed", enchantingTable, block.EnchantingTable{})
	s.clickBlock("the client opens the enchanting table", enchantingTable)
	s.refuse("refused: an option with nothing in the table", s.enchant(0, 1)...)

	s.request("a stick put in it", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 2), Destination: s.slot(v2193.ContainerEnchantingInput, uiEnchantingInput)})
	s.refuse("refused: the stick enchanted", s.enchant(0, 1)...)
	s.request("the stick taken back", nil,
		&v2193.TakeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerEnchantingInput, uiEnchantingInput), Destination: s.slot(inv, 2)})

	s.attempt("two books put in it",
		&v2193.PlaceStackRequestAction{Count: 2, Source: s.slot(inv, 1), Destination: s.slot(v2193.ContainerEnchantingInput, uiEnchantingInput)})
	s.refuse("refused: two books enchanted", s.enchant(0, 1)...)
	s.attempt("the books taken back",
		&v2193.TakeStackRequestAction{Count: 2, Source: s.slot(v2193.ContainerEnchantingInput, uiEnchantingInput), Destination: s.slot(inv, 1)})

	s.request("the pickaxe put in it: the options offered", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 0), Destination: s.slot(v2193.ContainerEnchantingInput, uiEnchantingInput)})
	s.refuse("refused: the first option with no levels", s.enchant(0, 1)...)
	s.step("a level given", func(_ *world.Tx, p *player.Player) { p.SetExperienceLevel(1) })
	s.refuse("refused: the third option with a level", s.enchant(2, 3)...)
	s.step("levels given", func(_ *world.Tx, p *player.Player) { p.SetExperienceLevel(30) })
	s.refuse("refused: the first option with no lapis", s.enchant(0, 1)...)
	s.refuse("refused: a fourth option", s.enchant(3, 1)...)
	s.request("a lapis put in it", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 3), Destination: s.slot(v2193.ContainerEnchantingMaterial, uiEnchantingLapis)})
	s.refuse("refused: the second option with a lapis", s.enchant(1, 2)...)
	s.refuse("refused: a lapis more put in, then the third option",
		append([]v2193.StackRequestAction{
			&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 3), Destination: s.slot(v2193.ContainerEnchantingMaterial, uiEnchantingLapis)},
		}, s.enchant(2, 3)...)...)
	s.request("a lapis more put in, then the second option taken", nil,
		append([]v2193.StackRequestAction{
			&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 3), Destination: s.slot(v2193.ContainerEnchantingMaterial, uiEnchantingLapis)},
		}, append(s.enchant(1, 2),
			&v2193.PlaceStackRequestAction{Count: 1, Source: s.created(), Destination: s.slot(inv, 0)})...)...)

	// The seed of the player is drawn at random now: the options the table
	// would offer next are its draws.
	s.normalise = randomEnchantOptions
	s.step("the table broken under the open menu", func(tx *world.Tx, _ *player.Player) {
		tx.SetBlock(enchantingTable, block.Stone{}, nil)
	})
	s.attempt("the first option taken with no table", s.enchant(0, 1)...)
	s.closeContainer("the client closes the enchanting table")
}

// enchantingTwiceCases takes an option of the enchanting table twice in a
// request: the second takes the result of the first again and is refused,
// after it paid. The refusal undoes the slots both changed, but not the
// levels paid. A later request names a stack the refused one made.
func (s *scenario) enchantingTwiceCases() {
	const inv = v2193.ContainerCombinedHotBarAndInventory
	s.give("items given",
		slotStack{0, item.NewStack(item.Pickaxe{Tier: item.ToolTierIron}, 1)},
		slotStack{1, item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1)},
		slotStack{3, item.NewStack(item.LapisLazuli{}, 5)},
	)
	s.step("the empty slot held and levels given", func(_ *world.Tx, p *player.Player) {
		if err := p.SetHeldSlot(emptyHand); err != nil {
			s.t.Error(err)
		}
		p.SetExperienceLevel(30)
	})
	s.place("an enchanting table is placed", enchantingTable, block.EnchantingTable{})
	s.clickBlock("the client opens the enchanting table", enchantingTable)
	s.request("the pickaxe and lapis put in it", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 0), Destination: s.slot(v2193.ContainerEnchantingInput, uiEnchantingInput)},
		&v2193.PlaceStackRequestAction{Count: 5, Source: s.slot(inv, 3), Destination: s.slot(v2193.ContainerEnchantingMaterial, uiEnchantingLapis)})
	// The first option resets the seed: the options the sword is offered
	// are drawn at random.
	s.normalise = randomEnchantOptions
	refused := s.nextRequest
	s.refuse("refused: the first option taken twice",
		append(s.enchant(0, 1),
			&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 1), Destination: s.again(v2193.ContainerEnchantingInput, uiEnchantingInput)},
			&v2193.CraftRecipeStackRequestAction{RecipeNetworkID: 0, NumberOfCrafts: 1})...)
	s.attempt("the lapis named by the refused request",
		&v2193.TakeStackRequestAction{Count: 1, Source: v2193.StackRequestSlotInfo{
			Container: v2193.FullContainerName{ContainerID: v2193.ContainerEnchantingMaterial}, Slot: uiEnchantingLapis, StackNetworkID: refused,
		}, Destination: s.slot(inv, 3)})
	// Closing a menu moves its inputs to the inventory, and the input of
	// the enchanting table then schedules its options, which a synchronous
	// world runs at once in a transaction of its own: Player.CloseMenu
	// would then close the menu in that transaction, ended. The pickaxe is
	// taken out first.
	s.attempt("the pickaxe taken back",
		&v2193.TakeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerEnchantingInput, uiEnchantingInput), Destination: s.slot(inv, 0)})
	s.closeContainer("the client closes the enchanting table")
}

// enchantingCreativeCases takes an option of an enchanting table among
// bookshelves in creative mode, which pays no levels and no lapis.
func (s *scenario) enchantingCreativeCases() {
	const inv = v2193.ContainerCombinedHotBarAndInventory
	s.give("items given", slotStack{0, item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1)})
	s.step("the empty slot held", func(_ *world.Tx, p *player.Player) {
		if err := p.SetHeldSlot(emptyHand); err != nil {
			s.t.Error(err)
		}
	})
	s.gameModeStep("game mode: creative", 1, func(_ *world.Tx, p *player.Player) {
		p.SetGameMode(world.GameModeCreative)
	})
	s.step("an enchanting table is placed among bookshelves", func(tx *world.Tx, _ *player.Player) {
		tx.SetBlock(enchantingTable, block.EnchantingTable{}, nil)
		for _, d := range []cube.Pos{{-2, 0, -2}, {-2, 0, 0}, {-2, 0, 2}, {-2, 1, -2}, {-2, 1, 0}, {0, 0, -2}, {0, 1, -2}} {
			tx.SetBlock(enchantingTable.Add(d), block.Bookshelf{}, nil)
		}
	})
	s.clickBlock("the client opens the enchanting table", enchantingTable)
	s.request("the sword put in it: the options offered", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 0), Destination: s.slot(v2193.ContainerEnchantingInput, uiEnchantingInput)})
	s.normalise = randomEnchantOptions
	s.request("the third option taken with no lapis and no levels", nil,
		append(s.enchant(2, 0),
			&v2193.PlaceStackRequestAction{Count: 1, Source: s.created(), Destination: s.slot(inv, 0)})...)
	s.closeContainer("the client closes the enchanting table")
}

// randomEnchantOptions normalises the options of the enchanting table that
// the client is sent once the enchantment seed of the player was reset to
// a seed drawn at random: their costs and enchantments differ from run to
// run.
func randomEnchantOptions(pk proto.Packet) (changed, omit bool) {
	opts, ok := pk.(*v2193.PlayerEnchantOptionsPacket)
	if !ok {
		return false, false
	}
	for i := range opts.Options {
		opts.Options[i].Cost = 0
		opts.Options[i].Enchantments = v2193.ItemEnchantments{Slot: opts.Options[i].Enchantments.Slot}
	}
	return true, false
}
