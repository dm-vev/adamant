package session_test

import (
	"reflect"
	"strings"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/bedrockdata"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// craftsScenario sends the requests that craft recipes of the crafting
// table and dynamic recipes, for TestMenuRequestScenarios: shaped, shapeless
// and user-data recipes, crafted once and many times, of one result and of
// two the client creates one by one, in the grid of the inventory and of a
// crafting table, refused for every check of the craft, a recipe locked by
// limited_crafting and unlocked, and the table replaced under its menu.
func (s *scenario) craftsScenario() {
	const (
		inv  = mc.ContainerCombinedHotBarAndInventory
		grid = mc.ContainerCraftingInput
	)
	planks := block.Planks{Wood: block.OakWood()}
	sticks := s.recipe("crafting_table", item.Stick{}, "planks")
	craftingTable := s.recipe("crafting_table", block.CraftingTable{}, "planks")
	oakPlanks := s.recipe("crafting_table", planks, "log")
	stoneBricks := s.recipe("stonecutter", block.StoneBricks{}, "minecraft:stone\"")
	blueBox := block.NewShulkerBox()
	blueBox.Type = block.BlueShulkerBox()
	dyeBox := s.userDataRecipe(blueBox, "red_shulker_box")
	craft := func(id uint32, times int) *mc.CraftRecipeStackRequestAction {
		return &mc.CraftRecipeStackRequestAction{RecipeNetworkID: id, NumberOfCrafts: byte(times)}
	}
	consume := func(slot, count int) *mc.ConsumeStackRequestAction {
		return &mc.ConsumeStackRequestAction{Count: byte(count), Source: s.slot(grid, slot)}
	}
	take := func(count, to int) *mc.PlaceStackRequestAction {
		return &mc.PlaceStackRequestAction{Count: byte(count), Source: s.created(), Destination: s.slot(inv, to)}
	}
	clearGrid := func(heading string) {
		s.step(heading, func(*world.Tx, *player.Player) {
			_, _, _, ui, _ := s.sess.Inventories()
			for slot := uiInventoryGrid; slot < uiCraftingGrid+9; slot++ {
				_ = ui.SetItem(slot, item.Stack{})
			}
		})
	}
	s.step("the empty slot held", func(_ *world.Tx, p *player.Player) {
		if err := p.SetHeldSlot(emptyHand); err != nil {
			s.t.Error(err)
		}
	})

	// The grid of the inventory.
	s.give("planks, logs and an apple given",
		slotStack{0, item.NewStack(planks, 16)},
		slotStack{1, item.NewStack(block.Log{Wood: block.OakWood()}, 20)},
		slotStack{3, item.NewStack(item.Apple{}, 5)},
	)
	s.openInventory("the client opens its inventory")
	s.fill("four planks put in the grid of the inventory", grid,
		[2]int{0, uiInventoryGrid}, [2]int{0, uiInventoryGrid + 1}, [2]int{0, uiInventoryGrid + 2}, [2]int{0, uiInventoryGrid + 3})
	s.request("a crafting table crafted, shaped", nil,
		craft(craftingTable, 1),
		consume(uiInventoryGrid, 1), consume(uiInventoryGrid+1, 1), consume(uiInventoryGrid+2, 1), consume(uiInventoryGrid+3, 1),
		take(1, 9))
	s.request("two planks each put in two slots of the grid", nil,
		&mc.PlaceStackRequestAction{Count: 2, Source: s.slot(inv, 0), Destination: s.slot(grid, uiInventoryGrid)},
		&mc.PlaceStackRequestAction{Count: 2, Source: s.again(inv, 0), Destination: s.slot(grid, uiInventoryGrid+2)})
	s.request("sticks crafted twice", nil,
		craft(sticks, 2), consume(uiInventoryGrid, 2), consume(uiInventoryGrid+2, 2), take(8, 10))
	s.refuse("refused: sticks of an empty grid", craft(sticks, 1))
	s.fill("a plank, an apple and a plank put in the grid", grid,
		[2]int{0, uiInventoryGrid}, [2]int{3, uiInventoryGrid + 1}, [2]int{0, uiInventoryGrid + 2})
	s.refuse("refused: sticks of a grid with an apple in it", craft(sticks, 1))
	s.request("the apple taken out of the grid", nil,
		&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(grid, uiInventoryGrid+1), Destination: s.slot(inv, 3)})
	s.refuse("refused: sticks crafted no times", craft(sticks, 0))
	s.refuse("refused: sticks crafted twice of one plank each", craft(sticks, 2))
	s.refuse("refused: a recipe of the stonecutter", craft(stoneBricks, 1))
	s.refuse("refused: sticks crafted, then a stale stack ID",
		craft(sticks, 1), consume(uiInventoryGrid, 1), consume(uiInventoryGrid+2, 1), take(4, 11),
		&mc.PlaceStackRequestAction{Count: 1, Source: mc.StackRequestSlotInfo{Container: mc.FullContainerName{ContainerID: inv}, Slot: 0, StackNetworkID: 9999}, Destination: s.slot(inv, 12)})
	s.refuse("refused: sticks crafted, then their single result created",
		craft(sticks, 1), &mc.CreateStackRequestAction{ResultsSlot: 0})
	s.request("sticks crafted", nil,
		craft(sticks, 1), consume(uiInventoryGrid, 1), consume(uiInventoryGrid+2, 1), take(4, 11))
	s.closeInventory("the client closes its inventory", 0)

	// The grid of a crafting table.
	table := cube.Pos{2, -60, 0}
	s.place("a crafting table is placed", table, block.CraftingTable{})
	s.clickBlock("the client opens the crafting table", table)
	s.request("seventeen logs put in the grid", nil,
		&mc.PlaceStackRequestAction{Count: 17, Source: s.slot(inv, 1), Destination: s.slot(grid, uiCraftingGrid+4)})
	s.request("planks crafted seventeen times: two results, created one by one", nil,
		craft(oakPlanks, 17), consume(uiCraftingGrid+4, 17),
		&mc.CreateStackRequestAction{ResultsSlot: 0}, take(64, 13),
		&mc.CreateStackRequestAction{ResultsSlot: 1}, take(4, 14))
	s.request("a log put in the grid", nil,
		&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 1), Destination: s.slot(grid, uiCraftingGrid)})
	s.refuse("refused: planks, a result created that is not there",
		craft(oakPlanks, 1), &mc.CreateStackRequestAction{ResultsSlot: 5})
	s.request("planks crafted", nil, craft(oakPlanks, 1), consume(uiCraftingGrid, 1), take(4, 15))

	// A shulker box dyed keeps what it holds and its name.
	s.step("a red shulker box of apples and blue dye given", func(_ *world.Tx, p *player.Player) {
		red := block.NewShulkerBox()
		red.Type = block.RedShulkerBox()
		_ = red.Inventory(nil, cube.Pos{}).SetItem(0, item.NewStack(item.Apple{}, 3))
		_ = p.Inventory().SetItem(16, item.NewStack(red, 1).WithCustomName("Supplies"))
		_ = p.Inventory().SetItem(17, item.NewStack(item.Dye{Colour: item.ColourBlue()}, 2))
	})
	s.fill("the shulker box and dye put in the grid", grid, [2]int{16, uiCraftingGrid}, [2]int{17, uiCraftingGrid + 1})
	s.request("the shulker box dyed blue, a user-data recipe", nil,
		craft(dyeBox, 1), consume(uiCraftingGrid, 1), consume(uiCraftingGrid+1, 1), take(1, 16))
	s.refuse("refused: a user-data recipe of no shulker box", craft(dyeBox, 1))
	clearGrid("the grid emptied")

	// A locked item is no input of a craft.
	s.step("a plank locked in the inventory put in the grid", func(*world.Tx, *player.Player) {
		_, _, _, ui, _ := s.sess.Inventories()
		_ = ui.SetItem(uiCraftingGrid, item.NewStack(planks, 1).WithLockMode(item.LockInInventory))
	})
	s.fill("a plank put in the grid", grid, [2]int{0, uiCraftingGrid + 3})
	s.refuse("refused: sticks of a locked plank", craft(sticks, 1))
	clearGrid("the grid emptied")

	// limited_crafting locks recipes the player has not unlocked.
	s.step("limited_crafting turned on", func(tx *world.Tx, _ *player.Player) {
		tx.World().SetGameRule("limited_crafting", true)
	})
	s.fill("two planks put in the grid", grid, [2]int{0, uiCraftingGrid}, [2]int{0, uiCraftingGrid + 3})
	s.refuse("refused: sticks not unlocked", craft(sticks, 1))
	s.step("sticks unlocked", func(_ *world.Tx, p *player.Player) { p.UnlockRecipes([]string{"minecraft:stick"}) })
	s.request("sticks crafted, unlocked", nil,
		craft(sticks, 1), consume(uiCraftingGrid, 1), consume(uiCraftingGrid+3, 1), take(4, 18))
	s.step("limited_crafting turned off", func(tx *world.Tx, _ *player.Player) {
		tx.World().SetGameRule("limited_crafting", false)
	})

	// Dynamic recipes, which the client names by no ID the server sent.
	const dynamic = 0xffff
	s.give("wolf armour and dye given",
		slotStack{19, item.NewStack(item.WolfArmor{}, 1)},
		slotStack{20, item.NewStack(item.Dye{Colour: item.ColourLime()}, 3)},
	)
	s.fill("wolf armour and dye put in the grid", grid, [2]int{19, uiCraftingGrid}, [2]int{20, uiCraftingGrid + 1})
	s.refuse("refused: the wolf armour dyed no times", craft(dynamic, 0))
	s.request("the wolf armour dyed three times, of one", nil,
		craft(dynamic, 3), consume(uiCraftingGrid, 1), consume(uiCraftingGrid+1, 1), take(1, 19))
	s.fill("an apple put in the grid", grid, [2]int{3, uiCraftingGrid + 4})
	s.refuse("refused: a dynamic recipe of an apple", craft(dynamic, 1))
	clearGrid("the grid emptied")

	// The table replaced under its menu: the grid is still its own.
	s.place("the crafting table replaced by stone", table, block.Stone{})
	s.fill("two planks put in the grid", grid, [2]int{0, uiCraftingGrid}, [2]int{0, uiCraftingGrid + 3})
	s.request("sticks crafted at the stone", nil,
		craft(sticks, 1), consume(uiCraftingGrid, 1), consume(uiCraftingGrid+3, 1), take(4, 21))
	s.closeContainer("the client closes the crafting table")
}

// userDataRecipe returns the network ID of the user-data shapeless recipe
// of the crafting table that the client was sent whose output is the item
// passed, of an input whose description holds input.
func (s *scenario) userDataRecipe(output world.Item, input string) uint32 {
	s.t.Helper()
	rid, _, _ := bedrockdata.ItemRuntimeID(output)
	for _, u := range s.crafting.UserDataShapelessRecipes {
		if r := u.ShapelessRecipe; r.Block == "crafting_table" && len(r.Output) == 1 && r.Output[0].NetworkID == rid && strings.Contains(inline(reflect.ValueOf(r.Input)), input) {
			return r.RecipeNetworkID
		}
	}
	s.t.Fatalf("no user-data recipe for %T of %s", output, input)
	return 0
}
