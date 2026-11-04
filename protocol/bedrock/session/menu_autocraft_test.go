package session_test

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// autoCraftScenario sends the requests of recipe-book crafts for
// TestGoldenMenuRequests, beyond those of the menus scenario: paid from the
// grid, the inventory or both, in the inventory screen and at a crafting
// table, crafted many times, of two results, of a user-data recipe and of a
// dynamic one, and refused for every check of the craft and its payments,
// with limited_crafting on, and with the table replaced under its menu.
func (s *scenario) autoCraftScenario() {
	const (
		inv  = v2193.ContainerCombinedHotBarAndInventory
		grid = v2193.ContainerCraftingInput
	)
	planks := block.Planks{Wood: block.OakWood()}
	sticks := s.recipe("crafting_table", item.Stick{}, "planks")
	oakPlanks := s.recipe("crafting_table", planks, "log")
	stoneBricks := s.recipe("stonecutter", block.StoneBricks{}, "minecraft:stone\"")
	blueBox := block.NewShulkerBox()
	blueBox.Type = block.BlueShulkerBox()
	dyeBox := s.userDataRecipe(blueBox, "red_shulker_box")
	auto := func(id uint32, times int) *v2193.AutoCraftRecipeStackRequestAction {
		return &v2193.AutoCraftRecipeStackRequestAction{RecipeNetworkID: id, NumberOfCrafts: byte(times)}
	}
	pay := func(container uint8, slot, count int) *v2193.ConsumeStackRequestAction {
		return &v2193.ConsumeStackRequestAction{Count: byte(count), Source: s.slot(container, slot)}
	}
	results := func(name string, times, count int) *v2193.CraftResultsDeprecatedStackRequestAction {
		return &v2193.CraftResultsDeprecatedStackRequestAction{TimesCrafted: byte(times), ResultItems: []v2193.StackRequestItem{{Identifier: name, Count: uint16(count)}}}
	}
	take := func(count, to int) *v2193.PlaceStackRequestAction {
		return &v2193.PlaceStackRequestAction{Count: byte(count), Source: s.created(), Destination: s.slot(inv, to)}
	}
	s.step("the empty slot held", func(_ *world.Tx, p *player.Player) {
		if err := p.SetHeldSlot(emptyHand); err != nil {
			s.t.Error(err)
		}
	})
	s.give("planks, logs, apples and locked planks given",
		slotStack{0, item.NewStack(planks, 32)},
		slotStack{1, item.NewStack(block.Log{Wood: block.OakWood()}, 20)},
		slotStack{2, item.NewStack(item.Apple{}, 5)},
		slotStack{3, item.NewStack(planks, 4).WithLockMode(item.LockInInventory)},
		slotStack{4, item.NewStack(planks, 1)},
	)

	// The inventory screen.
	s.openInventory("the client opens its inventory")
	s.request("recipe book: sticks of two planks of the inventory", nil,
		auto(sticks, 1), pay(inv, 0, 2), results("minecraft:stick", 1, 4), take(4, 9))
	s.request("recipe book: sticks three times of six planks, two slots", nil,
		auto(sticks, 3), pay(inv, 0, 5), pay(inv, 4, 1), results("minecraft:stick", 3, 12), take(12, 9))
	s.refuse("refused: sticks crafted no times", auto(sticks, 0), pay(inv, 0, 2))
	s.refuse("refused: sticks of one plank", auto(sticks, 1), pay(inv, 0, 1), results("minecraft:stick", 1, 4))
	s.refuse("refused: sticks of three planks", auto(sticks, 1), pay(inv, 0, 3), results("minecraft:stick", 1, 4))
	s.refuse("refused: sticks of a plank and an apple", auto(sticks, 1), pay(inv, 0, 1), pay(inv, 2, 1))
	s.refuse("refused: sticks paid with no plank", auto(sticks, 1), pay(inv, 0, 0), pay(inv, 0, 2))
	s.refuse("refused: sticks paid with more planks than there are", auto(sticks, 1), pay(inv, 4, 2))
	s.refuse("refused: sticks paid twice from a slot", auto(sticks, 1), pay(inv, 0, 1), pay(inv, 0, 1))
	s.refuse("refused: sticks of locked planks", auto(sticks, 1), pay(inv, 3, 2))
	s.refuse("refused: sticks paid from the cursor", auto(sticks, 1), pay(v2193.ContainerCursor, uiCursor, 2))
	s.refuse("refused: sticks paid from the armour", auto(sticks, 1), pay(v2193.ContainerArmor, 0, 2))
	s.refuse("refused: sticks paid from the hot bar", auto(sticks, 1), pay(v2193.ContainerHotBar, 0, 2))
	s.refuse("refused: sticks paid from the grid of the table", auto(sticks, 1), pay(grid, uiCraftingGrid, 2))
	s.refuse("refused: a recipe of the stonecutter", auto(stoneBricks, 1), pay(inv, 0, 1))
	s.refuse("refused: sticks paid with a stale stack ID", auto(sticks, 1),
		&v2193.ConsumeStackRequestAction{Count: 2, Source: v2193.StackRequestSlotInfo{Container: v2193.FullContainerName{ContainerID: inv}, Slot: 0, StackNetworkID: 9999}})
	s.refuse("refused: sticks paid, then a stale stack ID", auto(sticks, 1), pay(inv, 0, 2), results("minecraft:stick", 1, 4),
		&v2193.PlaceStackRequestAction{Count: 4, Source: s.created(), Destination: v2193.StackRequestSlotInfo{Container: v2193.FullContainerName{ContainerID: inv}, Slot: 10, StackNetworkID: 9999}})
	s.request("a plank put in the grid of the inventory", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 0), Destination: s.slot(grid, uiInventoryGrid+3)})
	s.request("recipe book: sticks of the plank of the grid and one of the inventory, the inventory first", nil,
		auto(sticks, 1), pay(inv, 0, 1), pay(grid, uiInventoryGrid+3, 1), results("minecraft:stick", 1, 4), take(4, 11))
	s.request("planks put in the grid of the inventory", nil,
		&v2193.PlaceStackRequestAction{Count: 2, Source: s.slot(inv, 0), Destination: s.slot(grid, uiInventoryGrid)})
	s.request("recipe book: sticks of the grid, the request ending with the payment", nil,
		auto(sticks, 1), pay(grid, uiInventoryGrid, 2))
	s.request("the sticks taken", nil,
		&v2193.PlaceStackRequestAction{Count: 4, Source: s.slot(v2193.ContainerCreatedOutput, uiCreatedOutput), Destination: s.slot(inv, 12)})
	s.closeInventory("the client closes its inventory", 0)
	s.refuse("refused: sticks with no screen open", auto(sticks, 1), pay(inv, 0, 2), results("minecraft:stick", 1, 4))

	// A crafting table.
	table := cube.Pos{2, -60, 0}
	s.place("a crafting table is placed", table, block.CraftingTable{})
	s.clickBlock("the client opens the crafting table", table)
	s.request("a plank put in the grid of the table", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 0), Destination: s.slot(grid, uiCraftingGrid+8)})
	s.refuse("refused: sticks paid from the grid of the inventory", auto(sticks, 1), pay(grid, uiInventoryGrid, 1), pay(inv, 0, 1))
	s.request("recipe book at the table: the plank of the grid and one of the inventory", nil,
		auto(sticks, 1), pay(grid, uiCraftingGrid+8, 1), pay(inv, 0, 1), results("minecraft:stick", 1, 4), take(4, 13))
	s.request("recipe book at the table: planks seventeen times, two results created one by one", nil,
		auto(oakPlanks, 17), pay(inv, 1, 17),
		&v2193.CreateStackRequestAction{ResultsSlot: 0}, take(64, 14),
		&v2193.CreateStackRequestAction{ResultsSlot: 1}, take(4, 15))
	s.step("a red shulker box of apples and blue dye given", func(_ *world.Tx, p *player.Player) {
		red := block.NewShulkerBox()
		red.Type = block.RedShulkerBox()
		_ = red.Inventory(nil, cube.Pos{}).SetItem(0, item.NewStack(item.Apple{}, 3))
		_ = p.Inventory().SetItem(16, item.NewStack(red, 1).WithCustomName("Supplies"))
		_ = p.Inventory().SetItem(17, item.NewStack(item.Dye{Colour: item.ColourBlue()}, 2))
	})
	s.request("recipe book at the table: the shulker box dyed blue, a user-data recipe", nil,
		auto(dyeBox, 1), pay(inv, 16, 1), pay(inv, 17, 1), results("minecraft:blue_shulker_box", 1, 1), take(1, 18))
	s.give("wolf armour and dye given",
		slotStack{19, item.NewStack(item.WolfArmor{}, 1)},
		slotStack{20, item.NewStack(item.Dye{Colour: item.ColourLime()}, 1)},
	)
	s.fill("wolf armour and dye put in the grid", grid, [2]int{19, uiCraftingGrid}, [2]int{20, uiCraftingGrid + 1})
	s.request("recipe book at the table: a dynamic recipe, the wolf armour dyed", nil,
		auto(0xffff, 1), pay(grid, uiCraftingGrid, 1), pay(grid, uiCraftingGrid+1, 1), take(1, 19))
	s.step("limited_crafting turned on", func(tx *world.Tx, _ *player.Player) {
		tx.World().SetGameRule("limited_crafting", true)
	})
	s.refuse("refused: sticks not unlocked", auto(sticks, 1), pay(inv, 0, 2), results("minecraft:stick", 1, 4))
	s.step("sticks unlocked", func(_ *world.Tx, p *player.Player) { p.UnlockRecipes([]string{"minecraft:stick"}) })
	s.request("recipe book at the table: sticks, unlocked", nil,
		auto(sticks, 1), pay(inv, 0, 2), results("minecraft:stick", 1, 4), take(4, 21))
	s.step("limited_crafting turned off", func(tx *world.Tx, _ *player.Player) {
		tx.World().SetGameRule("limited_crafting", false)
	})
	s.place("the crafting table replaced by stone", table, block.Stone{})
	s.request("recipe book at the stone", nil,
		auto(sticks, 1), pay(inv, 0, 2), results("minecraft:stick", 1, 4), take(4, 22))
	s.closeContainer("the client closes the crafting table")

	// A furnace is no crafting screen.
	furnace := cube.Pos{-1, -60, 2}
	s.place("a furnace is placed", furnace, block.NewFurnace(cube.North))
	s.clickBlock("the client opens the furnace", furnace)
	s.refuse("refused: sticks at the furnace", auto(sticks, 1), pay(inv, 0, 2), results("minecraft:stick", 1, 4))
	s.closeContainer("the client closes the furnace")
}
