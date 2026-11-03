package session_test

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/bedrockdata"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/creative"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// The multi recipes of the menus scenario, by the UUIDs the client is sent
// them under.
const (
	multiRepair              = "00000000-0000-0000-0000-000000000001"
	multiFirework            = "00000000-0000-0000-0000-000000000002"
	multiMapCloneCartograph  = "442d85ed-8272-4543-a6f1-418f90ded05d"
	multiMapLock             = "602234e4-cac1-4353-8bb7-b1ebff70024b"
	multiMapClone            = "85939755-ba10-4d9d-a4cc-efb7a8e943c4"
	multiMapExtendCartograph = "8b36268c-1829-483c-a0f1-993b7156a8f2"
	multiBannerDuplicate     = "b5c5d105-75a2-4076-af2b-923ea2bf4bf0"
	multiBookClone           = "d1ca6b84-338e-4f2f-9c6b-76cc8b4bd98d"
	multiMapExtend           = "d392b075-4ba1-40ae-8789-af868d56f6ce"
	multiBannerPattern       = "d81aaeaf-e172-4440-9225-868df030d27b"
)

// The UI slots of the menus the menus scenario uses beyond those of the
// inventory scenario, as the client names them.
const (
	uiInventoryGrid       = 28
	uiCartographyInput    = 12
	uiCartographyAddition = 13
)

// menusVillagerUUID is the UUID of the villager of the menus scenario.
var menusVillagerUUID = uuid.MustParse("00000000-0000-4000-8000-000000000f12")

// TestGoldenMenus records what a client is sent for the menu paths the
// inventory, multicraft and refusals scenarios leave out: recipe-book
// crafts from the inventory screen and the crafting table, paid from the
// grid and the inventory as the BDS capture of
// testdata/bedrock_1_26_52_autocraft.json has them; every special recipe
// of the crafting and cartography tables; a dynamic recipe; a recipe of two
// results the client creates one by one; a creative craft; the Destroy a
// beacon payment ignores; bundles filled and emptied; a trade taken from the
// result preview; a chest by a comparator, a barrel, a shulker box, a hopper
// and an ender chest opened, filled and closed; a furnace opened with items;
// a crafter whose slot is turned off and then filled; the lectern screen;
// the inventory screen closed as 0xff; and item stack requests refused for
// a locked item, the curse of binding, more items than a slot holds, a
// container that is not open and a forged result of a special recipe.
func TestGoldenMenus(t *testing.T) {
	if isolated(t) {
		return
	}
	s := newScenario(t, "menus", scenarioConfig{})
	s.menusScenario()
	s.snapshot()
}

// menusScenario runs the steps of TestGoldenMenus.
func (s *scenario) menusScenario() {
	s.step("the empty slot held", func(_ *world.Tx, p *player.Player) {
		if err := p.SetHeldSlot(emptyHand); err != nil {
			s.t.Error(err)
		}
	})
	s.recipeBookSteps()
	s.specialRecipeSteps()
	s.craftSteps()
	s.bundleSteps()
	s.tradePreviewSteps()
	s.containerSteps()
	s.crafterSteps()
	s.lecternSteps()
	s.menuRefusalSteps()
}

// give sets the slots of the inventory of the player, in order of slot.
func (s *scenario) give(heading string, items ...slotStack) {
	s.t.Helper()
	s.step(heading, func(_ *world.Tx, p *player.Player) {
		for _, it := range items {
			if err := p.Inventory().SetItem(it.slot, it.stack); err != nil {
				s.t.Fatal(err)
			}
		}
	})
}

// slotStack is a stack for a slot of the inventory.
type slotStack struct {
	slot  int
	stack item.Stack
}

// clearInventory empties the inventory of the player, so that the next part
// of a scenario starts from nothing.
func (s *scenario) clearInventory(heading string) {
	s.t.Helper()
	s.step(heading, func(_ *world.Tx, p *player.Player) {
		for slot, it := range p.Inventory().Slots() {
			if !it.Empty() {
				_ = p.Inventory().SetItem(slot, item.Stack{})
			}
		}
	})
}

// created returns the slot of the created output the client takes the
// result of the request being made from.
func (s *scenario) created() v2193.StackRequestSlotInfo {
	return v2193.StackRequestSlotInfo{Container: v2193.FullContainerName{ContainerID: v2193.ContainerCreatedOutput}, Slot: uiCreatedOutput, StackNetworkID: s.nextRequest}
}

// openInventory sends what the client sends when the player opens its
// inventory screen.
func (s *scenario) openInventory(heading string) {
	s.t.Helper()
	s.client(heading, &v2193.InteractPacket{ActionType: v2193.InteractActionOpenInventory, TargetEntityRuntimeID: 1})
}

// recipeBookSteps crafts sticks of planks with the recipe book, from the
// inventory screen and the crafting table, paid from the grid, the
// inventory or both, and once with no screen open, which is refused.
func (s *scenario) recipeBookSteps() {
	const inv = v2193.ContainerCombinedHotBarAndInventory
	sticks := s.recipe("crafting_table", item.Stick{}, "planks")
	result := func(n int) *v2193.CraftResultsDeprecatedStackRequestAction {
		return &v2193.CraftResultsDeprecatedStackRequestAction{TimesCrafted: byte(n / 4), ResultItems: []v2193.StackRequestItem{{Identifier: "minecraft:stick", Count: uint16(n)}}}
	}
	s.give("planks given", slotStack{0, item.NewStack(block.Planks{Wood: block.OakWood()}, 16)})

	s.openInventory("the client opens its inventory")
	// inventory/grid-empty of the capture.
	s.request("recipe book, inventory screen: two planks of the inventory", nil,
		&v2193.AutoCraftRecipeStackRequestAction{RecipeNetworkID: sticks, NumberOfCrafts: 1},
		&v2193.ConsumeStackRequestAction{Count: 2, Source: s.slot(inv, 0)},
		result(4),
		&v2193.PlaceStackRequestAction{Count: 4, Source: s.created(), Destination: s.slot(inv, 9)})
	s.request("a plank put in the grid of the inventory", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 0), Destination: s.slot(v2193.ContainerCraftingInput, uiInventoryGrid)})
	// inventory/consume-grid-then-inventory of the capture.
	s.request("recipe book, inventory screen: the plank of the grid, then one of the inventory", nil,
		&v2193.AutoCraftRecipeStackRequestAction{RecipeNetworkID: sticks, NumberOfCrafts: 1},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerCraftingInput, uiInventoryGrid)},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(inv, 0)},
		result(4),
		&v2193.PlaceStackRequestAction{Count: 4, Source: s.created(), Destination: s.slot(inv, 9)})
	s.closeInventory("the client closes its inventory", 0)

	table := cube.Pos{2, -60, 0}
	s.place("a crafting table is placed", table, block.CraftingTable{})
	s.clickBlock("the client opens the crafting table", table)
	s.request("a plank put in the grid of the table", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 0), Destination: s.slot(v2193.ContainerCraftingInput, uiCraftingGrid)})
	// table/consume-inventory-then-grid of the capture.
	s.request("recipe book, table: one plank of the inventory, then that of the grid", nil,
		&v2193.AutoCraftRecipeStackRequestAction{RecipeNetworkID: sticks, NumberOfCrafts: 1},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(inv, 0)},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerCraftingInput, uiCraftingGrid)},
		result(4),
		&v2193.PlaceStackRequestAction{Count: 4, Source: s.created(), Destination: s.slot(inv, 9)})
	s.request("a plank put in the grid of the table again", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 0), Destination: s.slot(v2193.ContainerCraftingInput, uiCraftingGrid)})
	// table/times-2-mixed of the capture.
	s.request("recipe book, table: two crafts, the plank of the grid and three of the inventory", nil,
		&v2193.AutoCraftRecipeStackRequestAction{RecipeNetworkID: sticks, NumberOfCrafts: 2},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerCraftingInput, uiCraftingGrid)},
		&v2193.ConsumeStackRequestAction{Count: 3, Source: s.slot(inv, 0)},
		result(8),
		&v2193.PlaceStackRequestAction{Count: 8, Source: s.created(), Destination: s.slot(inv, 10)})
	s.closeContainer("the client closes the crafting table")

	// closed/grid-empty of the capture: BDS refuses it (7).
	s.refuse("refused: recipe book with no screen open",
		&v2193.AutoCraftRecipeStackRequestAction{RecipeNetworkID: sticks, NumberOfCrafts: 1},
		&v2193.ConsumeStackRequestAction{Count: 2, Source: s.slot(inv, 0)},
		result(4),
		&v2193.PlaceStackRequestAction{Count: 4, Source: s.created(), Destination: s.slot(inv, 11)})
	s.clearInventory("the inventory emptied")
}

// closeInventory sends what the client sends when the player closes its
// inventory screen, as window 0 or, when it opened chat at the same time,
// as window 0xff.
func (s *scenario) closeInventory(heading string, window byte) {
	s.t.Helper()
	s.client(heading, &v2193.ContainerClosePacket{WindowID: window, ContainerType: 0xff})
}

// multiCraft sends the request of a special recipe of the UUID passed whose
// result is the stack passed, consuming one of each of the slots of the
// containers passed, and then the request that takes its result to a slot
// of the inventory.
func (s *scenario) multiCraft(heading, id string, result item.Stack, to int, from ...v2193.StackRequestSlotInfo) {
	s.t.Helper()
	name, meta := result.Item().EncodeItem()
	actions := []v2193.StackRequestAction{
		&v2193.CraftRecipeStackRequestAction{RecipeNetworkID: s.multiRecipe(id), NumberOfCrafts: 1},
		&v2193.CraftResultsDeprecatedStackRequestAction{TimesCrafted: 1, ResultItems: []v2193.StackRequestItem{{
			Identifier: name, MetadataValue: uint32(meta), Count: uint16(result.Count()), NBTData: item.WriteNBT(result, false),
		}}},
	}
	for _, slot := range from {
		actions = append(actions, &v2193.ConsumeStackRequestAction{Count: 1, Source: slot})
	}
	s.request(heading, nil, actions...)
	s.request(heading+": the result taken", nil,
		&v2193.PlaceStackRequestAction{Count: byte(result.Count()), Source: s.slot(v2193.ContainerCreatedOutput, uiCreatedOutput), Destination: s.slot(v2193.ContainerCombinedHotBarAndInventory, to)})
}

// fill sends the request that puts one item of each slot of the inventory
// passed in the slot of the container passed after it.
func (s *scenario) fill(heading string, container uint8, pairs ...[2]int) {
	s.t.Helper()
	const inv = v2193.ContainerCombinedHotBarAndInventory
	var actions []v2193.StackRequestAction
	taken := map[int]bool{}
	for _, p := range pairs {
		source := s.slot(inv, p[0])
		if taken[p[0]] {
			source = s.again(inv, p[0])
		}
		taken[p[0]] = true
		actions = append(actions, &v2193.PlaceStackRequestAction{Count: 1, Source: source, Destination: s.slot(container, p[1])})
	}
	s.request(heading, nil, actions...)
}

// specialRecipeSteps crafts every special recipe of the crafting table but
// the repair, which the multicraft scenario records, and those of the
// cartography table.
func (s *scenario) specialRecipeSteps() {
	const grid = v2193.ContainerCraftingInput
	var source item.Stack
	s.step("a map is made of the land around", func(tx *world.Tx, _ *player.Player) {
		data, err := tx.CreateMap(cube.Pos{0, -60, 0}, 0)
		if err != nil {
			s.t.Fatal(err)
		}
		source = item.NewStack(item.FilledMap{NativeID: data.ID, HasNativeID: true}, 1)
	})
	white := item.ColourWhite()
	patterned := block.Banner{Colour: white, Patterns: []block.BannerPatternLayer{{Type: block.BorderBannerPattern(), Colour: item.ColourRed()}}}
	book := item.WrittenBook{Title: "Golden", Author: goldenName, Generation: item.OriginalGeneration(), Pages: []string{"a page"}}
	s.give("the inputs of the special recipes given",
		slotStack{0, source.Grow(3)},
		slotStack{1, item.NewStack(item.EmptyMap{}, 3)},
		slotStack{2, item.NewStack(item.Paper{}, 16)},
		slotStack{4, item.NewStack(block.GlassPane{}, 1)},
		slotStack{5, item.NewStack(patterned, 1)},
		slotStack{6, item.NewStack(block.Banner{Colour: white}, 2)},
		slotStack{7, item.NewStack(item.Dye{Colour: item.ColourBlue()}, 3)},
		slotStack{9, item.NewStack(book, 1)},
		slotStack{10, item.NewStack(item.BookAndQuill{}, 1)},
		slotStack{11, item.NewStack(item.Gunpowder{}, 2)},
	)

	table := cube.Pos{2, -60, 0}
	s.clickBlock("the client opens the crafting table", table)

	s.fill("a map and an empty map put in the grid", grid, [2]int{0, uiCraftingGrid}, [2]int{1, uiCraftingGrid + 1})
	s.multiCraft("the map cloned at the table", multiMapClone, source.Grow(1), 20,
		s.slot(grid, uiCraftingGrid), s.slot(grid, uiCraftingGrid+1))

	paper := make([][2]int, 0, 9)
	for i := range 9 {
		if i == 4 {
			paper = append(paper, [2]int{20, uiCraftingGrid + i})
			continue
		}
		paper = append(paper, [2]int{2, uiCraftingGrid + i})
	}
	s.fill("a map in the middle of eight paper", grid, paper...)
	var all []v2193.StackRequestSlotInfo
	for i := range 9 {
		all = append(all, s.slot(grid, uiCraftingGrid+i))
	}
	s.multiCraft("the map extended at the table", multiMapExtend, source, 21, all...)

	s.fill("a patterned banner beside a blank one", grid, [2]int{5, uiCraftingGrid}, [2]int{6, uiCraftingGrid + 1})
	s.multiCraft("the banner duplicated", multiBannerDuplicate, item.NewStack(patterned, 2), 22,
		s.slot(grid, uiCraftingGrid), s.slot(grid, uiCraftingGrid+1))

	stripe := block.Banner{Colour: white, Patterns: []block.BannerPatternLayer{{Type: block.StripeBottomBannerPattern(), Colour: item.ColourBlue()}}}
	s.fill("a blank banner above a row of three dyes", grid,
		[2]int{6, uiCraftingGrid + 4}, [2]int{7, uiCraftingGrid + 6}, [2]int{7, uiCraftingGrid + 7}, [2]int{7, uiCraftingGrid + 8})
	s.multiCraft("a pattern added to the banner of the grid", multiBannerPattern, item.NewStack(stripe, 1), 23,
		s.slot(grid, uiCraftingGrid+4), s.slot(grid, uiCraftingGrid+6), s.slot(grid, uiCraftingGrid+7), s.slot(grid, uiCraftingGrid+8))

	copied := book
	copied.Generation = item.CopyGeneration()
	s.fill("a written book and a book and quill put in the grid", grid, [2]int{9, uiCraftingGrid}, [2]int{10, uiCraftingGrid + 1})
	s.multiCraft("the book cloned", multiBookClone, item.NewStack(copied, 2), 24,
		s.slot(grid, uiCraftingGrid), s.slot(grid, uiCraftingGrid+1))

	s.fill("paper and two gunpowder put in the grid", grid, [2]int{2, uiCraftingGrid}, [2]int{11, uiCraftingGrid + 1}, [2]int{11, uiCraftingGrid + 2})
	s.multiCraft("fireworks crafted", multiFirework, item.NewStack(item.Firework{Duration: 1500 * time.Millisecond, Explosions: []item.FireworkExplosion{}}, 3), 25,
		s.slot(grid, uiCraftingGrid), s.slot(grid, uiCraftingGrid+1), s.slot(grid, uiCraftingGrid+2))
	s.closeContainer("the client closes the crafting table")

	cartography := cube.Pos{-2, -60, 0}
	s.place("a cartography table is placed", cartography, block.CartographyTable{})
	s.clickBlock("the client opens the cartography table", cartography)
	cart := func(addition int) {
		s.request("a map and its addition put in the cartography table", nil,
			&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerCombinedHotBarAndInventory, 0), Destination: s.slot(v2193.ContainerCartographyInput, uiCartographyInput)},
			&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerCombinedHotBarAndInventory, addition), Destination: s.slot(v2193.ContainerCartographyAdditional, uiCartographyAddition)})
	}
	both := func() []v2193.StackRequestSlotInfo {
		return []v2193.StackRequestSlotInfo{s.slot(v2193.ContainerCartographyInput, uiCartographyInput), s.slot(v2193.ContainerCartographyAdditional, uiCartographyAddition)}
	}
	cart(1)
	s.multiCraft("the map cloned at the cartography table", multiMapCloneCartograph, source.Grow(1), 26, both()...)
	cart(2)
	s.multiCraft("the map extended at the cartography table", multiMapExtendCartograph, source, 27, both()...)
	cart(4)
	s.multiCraft("the map locked at the cartography table", multiMapLock, source, 28, both()...)
	s.closeContainer("the client closes the cartography table")
	s.clearInventory("the inventory emptied")
}

// craftSteps crafts a dynamic recipe, a recipe of two results and a
// creative item, destroys the creative item, and pays a beacon with the
// Destroy the client sends after, which the server ignores.
func (s *scenario) craftSteps() {
	const inv = v2193.ContainerCombinedHotBarAndInventory
	s.give("wolf armour, dye and honey given",
		slotStack{0, item.NewStack(item.WolfArmor{}, 1)},
		slotStack{1, item.NewStack(item.Dye{Colour: item.ColourLime()}, 1)},
		slotStack{2, item.NewStack(item.HoneyBottle{}, 1)},
		slotStack{3, item.NewStack(item.IronIngot{}, 1)},
	)
	table := cube.Pos{2, -60, 0}
	s.clickBlock("the client opens the crafting table", table)
	s.fill("wolf armour and dye put in the grid", v2193.ContainerCraftingInput, [2]int{0, uiCraftingGrid}, [2]int{1, uiCraftingGrid + 1})
	// The client names a dynamic recipe by no ID the server sent.
	s.request("the wolf armour dyed, a dynamic recipe", nil,
		&v2193.CraftRecipeStackRequestAction{RecipeNetworkID: 0xffff, NumberOfCrafts: 1},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerCraftingInput, uiCraftingGrid)},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerCraftingInput, uiCraftingGrid+1)},
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.created(), Destination: s.slot(inv, 0)})

	// A honey bottle makes three sugar and gives the bottle back: the client
	// creates each of the two results before it takes it.
	s.fill("a honey bottle put in the grid", v2193.ContainerCraftingInput, [2]int{2, uiCraftingGrid})
	s.request("sugar crafted: two results, created one by one", nil,
		&v2193.CraftRecipeStackRequestAction{RecipeNetworkID: s.twoResultRecipe(item.Sugar{}), NumberOfCrafts: 1},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerCraftingInput, uiCraftingGrid)},
		&v2193.CreateStackRequestAction{ResultsSlot: 0},
		&v2193.PlaceStackRequestAction{Count: 3, Source: s.created(), Destination: s.slot(inv, 4)},
		&v2193.CreateStackRequestAction{ResultsSlot: 1},
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.created(), Destination: s.slot(inv, 5)})
	s.closeContainer("the client closes the crafting table")

	// The beacon takes no pyramid to be paid without an effect; the client
	// destroys the payment after, which the server ignores, in survival
	// too.
	beacon := cube.Pos{-2, -60, 2}
	s.place("a beacon is placed", beacon, block.Beacon{})
	s.clickBlock("the client opens the beacon", beacon)
	s.request("an iron ingot put in the beacon", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 3), Destination: s.slot(v2193.ContainerBeaconPayment, uiBeaconPayment)})
	s.request("the beacon paid, the payment then destroyed", nil,
		&v2193.BeaconPaymentStackRequestAction{},
		&v2193.DestroyStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerBeaconPayment, uiBeaconPayment)})
	s.closeContainer("the client closes the beacon")

	stone := 0
	for i, it := range creative.Items() {
		if name, meta := it.Stack.Item().EncodeItem(); name == "minecraft:stone" && meta == 0 {
			stone = i + 1
			break
		}
	}
	if stone == 0 {
		s.t.Fatal("stone is not a creative item")
	}
	s.gameModeStep("game mode: creative", 1, func(_ *world.Tx, p *player.Player) {
		p.SetGameMode(world.GameModeCreative)
	})
	s.request("creative: a stack of stone taken", nil,
		&v2193.CraftCreativeStackRequestAction{CreativeItemNetworkID: uint32(stone), NumberOfCrafts: 1},
		&v2193.PlaceStackRequestAction{Count: 64, Source: s.created(), Destination: s.slot(inv, 6)})
	s.request("creative: the stone destroyed", nil,
		&v2193.DestroyStackRequestAction{Count: 64, Source: s.slot(inv, 6)})
	s.gameModeStep("game mode: survival", 1, func(_ *world.Tx, p *player.Player) {
		p.SetGameMode(world.GameModeSurvival)
	})
	s.clearInventory("the inventory emptied")
}

// bundleSteps puts apples in a bundle and takes one out again.
func (s *scenario) bundleSteps() {
	const inv = v2193.ContainerCombinedHotBarAndInventory
	s.give("a bundle and apples given",
		slotStack{0, item.NewStack(item.Bundle{}, 1)},
		slotStack{1, item.NewStack(item.Apple{}, 16)},
	)
	s.request("four apples put in the bundle", nil,
		&v2193.PlaceStackRequestAction{Count: 4, Source: s.slot(inv, 1), Destination: s.slot(inv, 0)})
	s.request("an apple taken out of the bundle onto the apples", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 0), Destination: s.slot(inv, 1)})
	s.clearInventory("the inventory emptied")
}

// tradePreviewSteps buys from a villager by taking the result of the
// preview, which pays from the inventory.
func (s *scenario) tradePreviewSteps() {
	const inv = v2193.ContainerCombinedHotBarAndInventory
	s.give("emeralds given", slotStack{0, item.NewStack(item.Emerald{}, 3)})
	var villager *world.EntityHandle
	s.step("a villager stands by", func(tx *world.Tx, p *player.Player) {
		h, err := entity.NewFromStructureNBT(map[string]any{
			"id":           "minecraft:villager",
			"NoAI":         byte(1),
			"VillagerData": map[string]any{"type": "minecraft:plains", "profession": "minecraft:farmer", "level": int32(1)},
			"Offers": map[string]any{"Recipes": []any{map[string]any{
				"buy":  item.WriteNBT(item.NewStack(item.Emerald{}, 1), true),
				"sell": item.WriteNBT(item.NewStack(item.Bread{}, 6), true),
				// No experience rewarded: its orbs fly off at random.
				"maxUses": int32(16), "xp": int32(2), "rewardExp": byte(0), "priceMultiplier": float32(0.05),
			}}},
		}, world.EntitySpawnOpts{Position: p.Position().Add(mgl64.Vec3{0, 0, -3}), ID: menusVillagerUUID, SpawnFinalized: true})
		if err != nil {
			s.t.Fatal(err)
		}
		villager = h
		tx.AddEntity(h)
	})
	s.client("the client opens the trade screen", &v2193.InventoryTransactionPacket{TransactionData: &v2193.UseItemOnEntityTransactionData{
		TargetEntityRuntimeID: s.runtimeID(villager),
		ActionType:            v2193.UseItemOnEntityActionInteract,
		HotBarSlot:            emptyHand,
		Position:              mgl32.Vec3{0.5, -58.38, 0.5},
		ClickedPosition:       mgl32.Vec3{0, 1, 0},
	}})
	s.request("bread taken from the preview of the first offer", nil,
		&v2193.TakeStackRequestAction{Count: 6, Source: v2193.StackRequestSlotInfo{Container: v2193.FullContainerName{ContainerID: v2193.ContainerTradeResultPreview}, Slot: 0}, Destination: s.slot(inv, 1)})
	s.closeContainer("the client closes the trade screen")
	s.step("the villager taken away", func(tx *world.Tx, _ *player.Player) {
		if e, ok := villager.Entity(tx); ok {
			tx.RemoveEntity(e)
		}
	})
	s.clearInventory("the inventory emptied")
}

// containerSteps opens the blocks that hold items, puts an apple in each
// and closes them; a comparator reads the chest. A furnace is opened with
// items in it and its input taken.
func (s *scenario) containerSteps() {
	const inv = v2193.ContainerCombinedHotBarAndInventory
	s.give("apples given", slotStack{0, item.NewStack(item.Apple{}, 16)})

	chest := cube.Pos{1, -60, -2}
	s.place("a chest is placed", chest, block.NewChest())
	s.place("a comparator reads the chest", chest.Add(cube.Pos{1, 0, 0}), block.RedstoneComparator{Facing: cube.West})
	// A shulker box opens upwards only if it faces up.
	shulkerBox := block.NewShulkerBox()
	shulkerBox.Facing = cube.FaceUp
	for _, c := range []struct {
		name      string
		pos       cube.Pos
		b         world.Block
		container uint8
	}{
		{"the chest", chest, nil, v2193.ContainerLevelEntity},
		{"a barrel", cube.Pos{-1, -60, -2}, block.NewBarrel(), v2193.ContainerBarrel},
		{"a shulker box", cube.Pos{-2, -60, -1}, shulkerBox, v2193.ContainerShulkerBox},
		{"a hopper", cube.Pos{-2, -60, 1}, block.NewHopper(), v2193.ContainerLevelEntity},
		{"an ender chest", cube.Pos{1, -60, 2}, block.NewEnderChest(), v2193.ContainerLevelEntity},
	} {
		if c.b != nil {
			s.place(c.name+" is placed", c.pos, c.b)
		}
		s.clickBlock("the client opens "+c.name, c.pos)
		s.request("an apple put in "+c.name, nil,
			&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 0), Destination: s.slot(c.container, 0)})
		s.tick("the tick after", 2)
		s.closeContainer("the client closes " + c.name)
		s.tick("the ticks after it closed", 2)
	}

	furnace := cube.Pos{-1, -60, 2}
	s.step("a furnace is placed with iron ore and iron in it", func(tx *world.Tx, _ *player.Player) {
		tx.SetBlock(furnace, block.NewFurnace(cube.North), nil)
		c, ok := tx.Block(furnace).(block.Container)
		if !ok {
			s.t.Fatal("the furnace holds no inventory")
		}
		_ = c.Inventory(tx, furnace).SetItem(0, item.NewStack(block.IronOre{}, 2))
		_ = c.Inventory(tx, furnace).SetItem(2, item.NewStack(item.IronIngot{}, 1))
	})
	s.clickBlock("the client opens the furnace", furnace)
	s.request("the iron ore taken out of the furnace", nil,
		&v2193.PlaceStackRequestAction{Count: 2, Source: s.slot(v2193.ContainerFurnaceIngredient, 0), Destination: s.slot(inv, 1)})
	s.closeContainer("the client closes the furnace")
	s.clearInventory("the inventory emptied")
}

// crafterSteps opens a crafter, turns its first slot off and fills it, which
// turns it on again.
func (s *scenario) crafterSteps() {
	const inv = v2193.ContainerCombinedHotBarAndInventory
	s.give("planks given", slotStack{0, item.NewStack(block.Planks{Wood: block.OakWood()}, 4)})
	crafter := cube.Pos{2, -60, 2}
	s.place("a crafter is placed", crafter, block.NewCrafter())
	s.clickBlock("the client opens the crafter", crafter)
	toggle := func(heading string, slot uint8, disabled bool) {
		s.client(heading, &v2193.PlayerToggleCrafterSlotRequestPacket{PosX: int32(crafter[0]), PosY: int32(crafter[1]), PosZ: int32(crafter[2]), Slot: slot, Disabled: disabled})
	}
	toggle("the client turns the first slot of the crafter off", 0, true)
	toggle("the client turns it off again", 0, true)
	toggle("the client turns the second slot off", 1, true)
	toggle("the client turns the second slot on", 1, false)
	s.request("a plank put in the slot turned off", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 0), Destination: s.slot(v2193.ContainerCrafterLevelEntity, 0)})
	s.closeContainer("the client closes the crafter")
	s.clearInventory("the inventory emptied")
}

// lecternSteps puts a book on a lectern, opens its screen and turns its
// page.
func (s *scenario) lecternSteps() {
	lectern := cube.Pos{0, -60, 2}
	book := item.NewStack(item.WrittenBook{Title: "Lectern", Author: goldenName, Pages: []string{"one", "two", "three"}}, 1)
	s.place("a lectern is placed with a book on it", lectern, block.Lectern{Book: book})
	s.clickBlock("the client opens the lectern", lectern)
	s.client("the client turns to the second page", &v2193.LecternUpdatePacket{Page: 1, PageCount: 3, Position: [3]int32{int32(lectern[0]), int32(lectern[1]), int32(lectern[2])}})
	s.tick("the tick after", 1)

	s.openInventory("the client opens its inventory")
	s.closeInventory("the client closes its inventory as it opens chat", 0xff)
}

// menuRefusalSteps sends the requests the server refuses that the refusals
// scenario leaves out.
func (s *scenario) menuRefusalSteps() {
	const inv = v2193.ContainerCombinedHotBarAndInventory
	sword := item.Sword{Tier: item.ToolTierWood}
	s.give("a locked apple, apples and damaged swords given",
		slotStack{0, item.NewStack(item.Apple{}, 1).WithLockMode(item.LockInSlot)},
		slotStack{1, item.NewStack(item.Apple{}, 16)},
		slotStack{2, item.NewStack(sword, 1).Damage(40)},
		slotStack{3, item.NewStack(sword, 1).Damage(40)},
	)
	s.step("a helmet of the curse of binding worn", func(_ *world.Tx, p *player.Player) {
		p.Armour().SetHelmet(item.NewStack(item.Helmet{Tier: item.ArmourTierLeather{}}, 1).WithEnchantments(item.NewEnchantment(enchantment.CurseOfBinding, 1)))
	})
	s.refuse("refused: the locked apple dropped",
		&v2193.DropStackRequestAction{Count: 1, Source: s.slot(inv, 0)})
	s.refuse("refused: the locked apple moved",
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 0), Destination: s.slot(inv, 9)})
	s.refuse("refused: the cursed helmet taken off",
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerArmor, 0), Destination: s.slot(inv, 9)})
	s.refuse("refused: twenty apples taken of sixteen",
		&v2193.PlaceStackRequestAction{Count: 20, Source: s.slot(inv, 1), Destination: s.slot(inv, 9)})
	s.refuse("refused: an apple put in a furnace not open",
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 1), Destination: s.slot(v2193.ContainerFurnaceIngredient, 0)})

	table := cube.Pos{2, -60, 0}
	s.clickBlock("the client opens the crafting table", table)
	s.fill("the swords put in the grid", v2193.ContainerCraftingInput, [2]int{2, uiCraftingGrid}, [2]int{3, uiCraftingGrid + 1})
	forged := item.NewStack(sword, 1)
	name, meta := forged.Item().EncodeItem()
	s.refuse("refused: the swords repaired to a sword as good as new",
		&v2193.CraftRecipeStackRequestAction{RecipeNetworkID: s.multiRecipe(multiRepair), NumberOfCrafts: 1},
		&v2193.CraftResultsDeprecatedStackRequestAction{TimesCrafted: 1, ResultItems: []v2193.StackRequestItem{{
			Identifier: name, MetadataValue: uint32(meta), Count: 1, NBTData: item.WriteNBT(forged, false),
		}}},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerCraftingInput, uiCraftingGrid)},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerCraftingInput, uiCraftingGrid+1)})
	s.closeContainer("the client closes the crafting table")
}

// twoResultRecipe returns the network ID of the shaped recipe of the
// crafting table of two results, the first of which is the item passed.
func (s *scenario) twoResultRecipe(first world.Item) uint32 {
	s.t.Helper()
	rid, _, _ := bedrockdata.ItemRuntimeID(first)
	for _, r := range s.crafting.ShapedRecipes {
		if r.Block == "crafting_table" && len(r.Output) == 2 && r.Output[0].NetworkID == rid {
			return r.RecipeNetworkID
		}
	}
	s.t.Fatalf("no recipe of two results for %T", first)
	return 0
}
