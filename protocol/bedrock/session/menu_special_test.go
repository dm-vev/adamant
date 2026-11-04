package session_test

import (
	"time"

	"github.com/df-mc/dragonfly/protocol/bedrock/session"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// specialScenario sends the requests that craft special recipes, Bedrock's
// multi recipes, for TestMenuRequestScenarios, beyond those of the menus and
// multicraft scenarios: crafted in the grid of the inventory, more than
// once, and refused for every check of the consumption, the result the
// client names and the snapshot, at the crafting table and the cartography
// table, and with the cartography table replaced under its menu.
func (s *scenario) specialScenario() {
	const (
		inv  = mc.ContainerCombinedHotBarAndInventory
		grid = mc.ContainerCraftingInput
	)
	bow := func(durability int) item.Stack { return item.NewStack(item.Bow{}, 1).WithDurability(durability) }
	resultOf := func(it item.Stack, times int) *mc.CraftResultsDeprecatedStackRequestAction {
		name, meta := it.Item().EncodeItem()
		return &mc.CraftResultsDeprecatedStackRequestAction{TimesCrafted: byte(times), ResultItems: []mc.StackRequestItem{{
			Identifier: name, MetadataValue: uint32(meta), Count: uint16(it.Count()), NBTData: session.NetworkItemNBT(it),
		}}}
	}
	craft := func(id string, times int) *mc.CraftRecipeStackRequestAction {
		return &mc.CraftRecipeStackRequestAction{RecipeNetworkID: s.multiRecipe(id), NumberOfCrafts: byte(times)}
	}
	consume := func(container uint8, slot, count int) *mc.ConsumeStackRequestAction {
		return &mc.ConsumeStackRequestAction{Count: byte(count), Source: s.slot(container, slot)}
	}
	take := func(count, to int) *mc.PlaceStackRequestAction {
		return &mc.PlaceStackRequestAction{Count: byte(count), Source: s.slot(mc.ContainerCreatedOutput, uiCreatedOutput), Destination: s.slot(inv, to)}
	}
	clearUI := func(heading string) {
		s.step(heading, func(*world.Tx, *player.Player) {
			_, _, _, ui, _ := s.sess.Inventories()
			for slot := uiCartographyInput; slot < uiCraftingGrid+9; slot++ {
				_ = ui.SetItem(slot, item.Stack{})
			}
		})
	}
	s.step("the empty slot held", func(_ *world.Tx, p *player.Player) {
		if err := p.SetHeldSlot(emptyHand); err != nil {
			s.t.Error(err)
		}
	})

	// The grid of the inventory: two bows repaired into one, which keeps
	// a twentieth more.
	repaired := bow(10 + 20 + item.NewStack(item.Bow{}, 1).MaxDurability()/20)
	s.give("bows, apples, paper and gunpowder given",
		slotStack{0, bow(10)},
		slotStack{1, bow(20)},
		slotStack{2, item.NewStack(item.Apple{}, 8)},
		slotStack{3, item.NewStack(item.Paper{}, 8)},
		slotStack{4, item.NewStack(item.Gunpowder{}, 8)},
		slotStack{5, bow(30).WithCustomName("valuable")},
	)
	s.openInventory("the client opens its inventory")
	s.fill("two bows put in the grid of the inventory", grid, [2]int{0, uiInventoryGrid}, [2]int{1, uiInventoryGrid + 1})
	s.request("the bows repaired in the grid of the inventory", nil,
		craft(multiRepair, 1), resultOf(repaired, 1),
		consume(grid, uiInventoryGrid+1, 1), consume(grid, uiInventoryGrid, 1))
	s.request("the repaired bow taken", nil, take(1, 0))
	s.fill("the repaired bow and a bow put in the grid", grid, [2]int{0, uiInventoryGrid}, [2]int{5, uiInventoryGrid + 1})
	both := func() []mc.StackRequestAction {
		return []mc.StackRequestAction{consume(grid, uiInventoryGrid, 1), consume(grid, uiInventoryGrid+1, 1)}
	}
	again := bow(min(item.NewStack(item.Bow{}, 1).MaxDurability(), repaired.Durability()+30+item.NewStack(item.Bow{}, 1).MaxDurability()/20))
	s.refuse("refused: no result named", append([]mc.StackRequestAction{craft(multiRepair, 1)}, both()...)...)
	s.refuse("refused: two results named", append([]mc.StackRequestAction{craft(multiRepair, 1), resultOf(again, 1), resultOf(again, 1)}, both()...)...)
	s.refuse("refused: the result named for two crafts", append([]mc.StackRequestAction{craft(multiRepair, 1), resultOf(again, 2)}, both()...)...)
	s.refuse("refused: a result of no items", craft(multiRepair, 1), &mc.CraftResultsDeprecatedStackRequestAction{TimesCrafted: 1, ResultItems: []mc.StackRequestItem{{Identifier: "minecraft:bow"}}})
	s.refuse("refused: two bows of one repair", append([]mc.StackRequestAction{craft(multiRepair, 1), resultOf(again.Grow(1), 1)}, both()...)...)
	s.refuse("refused: the repair crafted no times", craft(multiRepair, 0))
	s.refuse("refused: the repair crafted twice", append([]mc.StackRequestAction{craft(multiRepair, 2), resultOf(again, 2)}, both()...)...)
	s.refuse("refused: one bow of two consumed", craft(multiRepair, 1), resultOf(again, 1), consume(grid, uiInventoryGrid, 1))
	s.refuse("refused: a bow consumed twice", craft(multiRepair, 1), resultOf(again, 1), consume(grid, uiInventoryGrid, 1), consume(grid, uiInventoryGrid, 1))
	s.refuse("refused: no bow consumed", craft(multiRepair, 1), resultOf(again, 1))
	s.refuse("refused: a bow consumed as none", craft(multiRepair, 1), resultOf(again, 1), consume(grid, uiInventoryGrid, 0), consume(grid, uiInventoryGrid+1, 1))
	s.refuse("refused: a slot of the table's grid consumed",
		craft(multiRepair, 1), resultOf(again, 1), consume(grid, uiInventoryGrid, 1), consume(grid, uiCraftingGrid, 1))
	s.refuse("refused: a slot of the grid named as the inventory",
		craft(multiRepair, 1), resultOf(again, 1), consume(grid, uiInventoryGrid, 1), consume(inv, uiInventoryGrid+1, 1))
	s.refuse("refused: an empty slot of the grid consumed",
		craft(multiRepair, 1), resultOf(again, 1), consume(grid, uiInventoryGrid, 1), consume(grid, uiInventoryGrid+1, 1), consume(grid, uiInventoryGrid+2, 1))
	s.refuse("refused: a bow of a stale stack ID", craft(multiRepair, 1), resultOf(again, 1),
		&mc.ConsumeStackRequestAction{Count: 1, Source: mc.StackRequestSlotInfo{Container: mc.FullContainerName{ContainerID: grid}, Slot: uiInventoryGrid, StackNetworkID: 9999}})
	s.refuse("refused: the repair, then an item moved",
		append(append([]mc.StackRequestAction{craft(multiRepair, 1), resultOf(again, 1)}, both()...), take(1, 9))...)
	s.request("an apple put in the grid with the bows", nil,
		&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 2), Destination: s.slot(grid, uiInventoryGrid+2)})
	s.refuse("refused: the bows repaired, the apple left in the grid", append([]mc.StackRequestAction{craft(multiRepair, 1), resultOf(again, 1)}, both()...)...)
	s.refuse("refused: the bows and the apple repaired",
		append(append([]mc.StackRequestAction{craft(multiRepair, 1), resultOf(again, 1)}, both()...), consume(grid, uiInventoryGrid+2, 1))...)
	s.refuse("refused: the apple and a bow consumed", craft(multiRepair, 1), resultOf(again, 1), consume(grid, uiInventoryGrid+1, 1), consume(grid, uiInventoryGrid+2, 1))
	s.closeInventory("the client closes its inventory", 0)

	// Fireworks crafted twice take two of each input.
	table := cube.Pos{2, -60, 0}
	s.place("a crafting table is placed", table, block.CraftingTable{})
	s.clickBlock("the client opens the crafting table", table)
	firework := item.NewStack(item.Firework{Duration: 1000 * time.Millisecond, Explosions: []item.FireworkExplosion{}}, 6)
	s.request("paper and gunpowder put in the grid, two each", nil,
		&mc.PlaceStackRequestAction{Count: 2, Source: s.slot(inv, 3), Destination: s.slot(grid, uiCraftingGrid)},
		&mc.PlaceStackRequestAction{Count: 2, Source: s.slot(inv, 4), Destination: s.slot(grid, uiCraftingGrid+4)})
	s.refuse("refused: fireworks crafted twice, one of each consumed",
		craft(multiFirework, 2), resultOf(firework, 2), consume(grid, uiCraftingGrid, 1), consume(grid, uiCraftingGrid+4, 1))
	s.refuse("refused: fireworks crafted once, two of each consumed",
		craft(multiFirework, 1), resultOf(firework.Grow(-3), 1), consume(grid, uiCraftingGrid, 2), consume(grid, uiCraftingGrid+4, 2))
	s.refuse("refused: fireworks crafted twice, three paper consumed",
		craft(multiFirework, 2), resultOf(firework, 2), consume(grid, uiCraftingGrid, 3), consume(grid, uiCraftingGrid+4, 2))
	s.request("fireworks crafted twice", nil,
		craft(multiFirework, 2), resultOf(firework, 2), consume(grid, uiCraftingGrid+4, 2), consume(grid, uiCraftingGrid, 2))
	s.request("the fireworks taken", nil, take(6, 10))

	// A locked item is no input.
	s.step("two bows put in the grid, one locked in the inventory", func(*world.Tx, *player.Player) {
		_, _, _, ui, _ := s.sess.Inventories()
		_ = ui.SetItem(uiCraftingGrid, bow(10).WithLockMode(item.LockInInventory))
		_ = ui.SetItem(uiCraftingGrid+1, bow(20))
	})
	s.refuse("refused: the locked bow repaired",
		craft(multiRepair, 1), resultOf(bow(10+20+item.NewStack(item.Bow{}, 1).MaxDurability()/20), 1),
		consume(grid, uiCraftingGrid, 1), consume(grid, uiCraftingGrid+1, 1))
	clearUI("the grid emptied")

	// Maps at the crafting table and the cartography table.
	var source item.Stack
	s.step("a map is made of the land around", func(tx *world.Tx, _ *player.Player) {
		data, err := tx.CreateMap(cube.Pos{0, -60, 0}, 0)
		if err != nil {
			s.t.Fatal(err)
		}
		source = item.NewStack(item.FilledMap{NativeID: data.ID, HasNativeID: true}, 1)
	})
	s.give("maps, paper and a glass pane given",
		slotStack{11, source.Grow(3)},
		slotStack{12, item.NewStack(item.EmptyMap{}, 3)},
		slotStack{13, item.NewStack(block.GlassPane{}, 2)},
	)
	s.fill("a map and two empty maps put in the grid", mc.ContainerCraftingInput,
		[2]int{11, uiCraftingGrid}, [2]int{12, uiCraftingGrid + 1}, [2]int{12, uiCraftingGrid + 2})
	s.refuse("refused: the map locked at the crafting table",
		craft(multiMapLock, 1), resultOf(source, 1), consume(grid, uiCraftingGrid, 1), consume(grid, uiCraftingGrid+1, 1), consume(grid, uiCraftingGrid+2, 1))
	s.request("the map cloned twice at the table", nil,
		craft(multiMapClone, 1), resultOf(source.Grow(2), 1), consume(grid, uiCraftingGrid, 1), consume(grid, uiCraftingGrid+1, 1), consume(grid, uiCraftingGrid+2, 1))
	s.request("the maps taken", nil, take(3, 14))
	s.closeContainer("the client closes the crafting table")

	cartography := cube.Pos{-2, -60, 0}
	s.place("a cartography table is placed", cartography, block.CartographyTable{})
	s.clickBlock("the client opens the cartography table", cartography)
	s.request("a map and a glass pane put in the cartography table", nil,
		&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 11), Destination: s.slot(mc.ContainerCartographyInput, uiCartographyInput)},
		&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 13), Destination: s.slot(mc.ContainerCartographyAdditional, uiCartographyAddition)})
	cart := func() []mc.StackRequestAction {
		return []mc.StackRequestAction{consume(mc.ContainerCartographyInput, uiCartographyInput, 1), consume(mc.ContainerCartographyAdditional, uiCartographyAddition, 1)}
	}
	s.refuse("refused: the repair at the cartography table", append([]mc.StackRequestAction{craft(multiRepair, 1), resultOf(source, 1)}, cart()...)...)
	s.refuse("refused: the map extended of a glass pane", append([]mc.StackRequestAction{craft(multiMapExtendCartograph, 1), resultOf(source, 1)}, cart()...)...)
	s.refuse("refused: the glass pane named as the map's slot",
		craft(multiMapLock, 1), resultOf(source, 1), consume(mc.ContainerCartographyInput, uiCartographyInput, 1), consume(mc.ContainerCartographyInput, uiCartographyAddition, 1))
	s.refuse("refused: a slot of the grid consumed at the cartography table",
		craft(multiMapLock, 1), resultOf(source, 1), consume(mc.ContainerCartographyInput, uiCartographyInput, 1), consume(grid, uiCraftingGrid, 1))
	s.refuse("refused: the map locked into two maps", append([]mc.StackRequestAction{craft(multiMapLock, 1), resultOf(source.Grow(1), 1)}, cart()...)...)
	s.request("the map locked at the cartography table", nil, append([]mc.StackRequestAction{craft(multiMapLock, 1), resultOf(source, 1)}, cart()...)...)
	s.request("the locked map taken", nil, take(1, 15))
	s.request("a map and a glass pane put in the cartography table again", nil,
		&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 11), Destination: s.slot(mc.ContainerCartographyInput, uiCartographyInput)},
		&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 13), Destination: s.slot(mc.ContainerCartographyAdditional, uiCartographyAddition)})
	s.place("the cartography table replaced by stone", cartography, block.Stone{})
	s.refuse("refused: the map locked at the stone", append([]mc.StackRequestAction{craft(multiMapLock, 1), resultOf(source, 1)}, cart()...)...)
	s.closeContainer("the client closes the stone")
}
