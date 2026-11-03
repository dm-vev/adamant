package session_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/bedrockdata"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// The UI slots of the containers of the menus, as the client names them.
const (
	uiCursor           = 0
	uiAnvilInput       = 1
	uiAnvilMaterial    = 2
	uiStonecutterInput = 3
	uiLoomInput        = 9
	uiLoomDye          = 10
	uiEnchantingInput  = 14
	uiEnchantingLapis  = 15
	uiGrindstoneFirst  = 16
	uiGrindstoneSecond = 17
	uiBeaconPayment    = 27
	uiCraftingGrid     = 32
	uiCreatedOutput    = 50
	uiSmithingInput    = 51
	uiSmithingMaterial = 52
	uiSmithingTemplate = 53
	uiTradeInput       = 4
)

// villagerUUID is the UUID of the villager of the inventory scenario.
var villagerUUID = uuid.MustParse("00000000-0000-4000-8000-000000000f11")

// emptyHand is the hot bar slot the player of the inventory scenario holds,
// left empty so that a click on a block opens it.
const emptyHand = 8

// TestGoldenInventory records what a client is sent for the requests it
// sends from its inventory and from menus: items taken, placed, swapped,
// dropped and destroyed; a recipe of the crafting table; an enchantment of
// the enchanting table with the options it offers; a repair of the
// grindstone; the stonecutter; a netherite upgrade of the smithing table;
// a pattern of the loom; the payment of a beacon; a trade with a
// villager; a book written in; a sign written on; and a rename and repair
// of the anvil.
func TestGoldenInventory(t *testing.T) {
	if isolated(t) {
		return
	}
	s := newScenario(t, "inventory", scenarioConfig{})
	s.inventoryScenario()
	s.snapshot()
}

// inventoryScenario runs the steps of TestGoldenInventory.
func (s *scenario) inventoryScenario() {

	s.step("items given", func(_ *world.Tx, p *player.Player) {
		inv := p.Inventory()
		// In order of slot: a map would give them in no order.
		for _, it := range []struct {
			slot  int
			stack item.Stack
		}{
			{0, item.NewStack(item.Apple{}, 16)},
			{1, item.NewStack(block.Stone{}, 4)},
			{2, item.NewStack(block.Planks{Wood: block.OakWood()}, 8)},
			{3, item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1).Damage(100)},
			{4, item.NewStack(item.Sword{Tier: item.ToolTierWood}, 1).Damage(30)},
			{5, item.NewStack(item.Sword{Tier: item.ToolTierWood}, 1).Damage(20)},
			{6, item.NewStack(block.Stone{}, 2)},
			{7, item.NewStack(item.Pickaxe{Tier: item.ToolTierIron}, 1)},
			{9, item.NewStack(item.SmithingTemplate{Template: item.TemplateNetheriteUpgrade()}, 1)},
			{10, item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1)},
			{11, item.NewStack(item.NetheriteIngot{}, 1)},
			{12, item.NewStack(block.Banner{Colour: item.ColourWhite()}, 1)},
			{13, item.NewStack(item.Dye{Colour: item.ColourRed()}, 1)},
			{14, item.NewStack(item.IronIngot{}, 1)},
			{15, item.NewStack(item.BookAndQuill{}, 1)},
			{16, item.NewStack(item.Emerald{}, 3)},
			{17, item.NewStack(item.LapisLazuli{}, 3)},
			{18, item.NewStack(item.Diamond{}, 1)},
		} {
			_ = inv.SetItem(it.slot, it.stack)
		}
	})
	s.step("the empty slot held", func(_ *world.Tx, p *player.Player) {
		if err := p.SetHeldSlot(emptyHand); err != nil {
			s.t.Error(err)
		}
	})
	s.inventorySteps()
	s.menuSteps()
}

// inventorySteps moves items within the inventory, as the inventory screen
// of the client does.
func (s *scenario) inventorySteps() {
	const inv = v2193.ContainerCombinedHotBarAndInventory
	s.request("take: half the apples to the cursor", nil,
		&v2193.TakeStackRequestAction{Count: 8, Source: s.slot(inv, 0), Destination: s.slot(v2193.ContainerCursor, uiCursor)})
	s.request("place: the apples of the cursor in an empty slot", nil,
		&v2193.PlaceStackRequestAction{Count: 8, Source: s.slot(v2193.ContainerCursor, uiCursor), Destination: s.slot(inv, 20)})
	s.request("swap: the apples and the stone", nil,
		&v2193.SwapStackRequestAction{Source: s.slot(inv, 20), Destination: s.slot(inv, 1)})
	s.request("drop: two apples", nil,
		&v2193.DropStackRequestAction{Count: 2, Source: s.slot(inv, 0)})
	s.tick("the dropped apples in the next tick", 1)
	s.step("the dropped apples taken away", func(tx *world.Tx, _ *player.Player) {
		for e := range tx.Entities() {
			if e.H().Type() == entity.ItemType {
				tx.RemoveEntity(e)
			}
		}
	})
}

// menuSteps opens the blocks of menus and uses them, as the client does.
func (s *scenario) menuSteps() {
	const inv = v2193.ContainerCombinedHotBarAndInventory
	created := func() v2193.StackRequestSlotInfo {
		return v2193.StackRequestSlotInfo{Container: v2193.FullContainerName{ContainerID: v2193.ContainerCreatedOutput}, Slot: uiCreatedOutput, StackNetworkID: s.nextRequest}
	}

	// The crafting table: two planks make four sticks.
	table := cube.Pos{2, -60, 0}
	s.place("a crafting table is placed", table, block.CraftingTable{})
	s.clickBlock("the client opens the crafting table", table)
	s.request("two planks put in the grid", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 2), Destination: s.slot(v2193.ContainerCraftingInput, uiCraftingGrid)},
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.again(inv, 2), Destination: s.slot(v2193.ContainerCraftingInput, uiCraftingGrid+3)})
	s.request("sticks crafted", nil,
		&v2193.CraftRecipeStackRequestAction{RecipeNetworkID: s.recipe("crafting_table", item.Stick{}, "planks"), NumberOfCrafts: 1},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerCraftingInput, uiCraftingGrid)},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerCraftingInput, uiCraftingGrid+3)},
		&v2193.PlaceStackRequestAction{Count: 4, Source: created(), Destination: s.slot(inv, 21)})
	s.closeContainer("the client closes the crafting table")

	// The enchanting table, with experience to pay for an enchantment.
	enchanting := cube.Pos{-2, -60, 0}
	s.place("an enchanting table is placed", enchanting, block.EnchantingTable{})
	s.step("experience given", func(_ *world.Tx, p *player.Player) { p.SetExperienceLevel(30) })
	s.clickBlock("the client opens the enchanting table", enchanting)
	s.request("a pickaxe put in it: the options offered", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 7), Destination: s.slot(v2193.ContainerEnchantingInput, uiEnchantingInput)})
	s.request("lapis put in it", nil,
		&v2193.PlaceStackRequestAction{Count: 3, Source: s.slot(inv, 17), Destination: s.slot(v2193.ContainerEnchantingMaterial, uiEnchantingLapis)})
	if len(s.enchantOptions) != 3 {
		s.t.Fatal("the enchanting table offered no options")
	}
	// The third option costs three lapis, all there is: what is left of an
	// input goes back to the inventory, to the empty hand.
	option := s.enchantOptions[2]
	s.request("the third option taken", nil,
		&v2193.CraftRecipeStackRequestAction{RecipeNetworkID: option.RecipeNetworkID, NumberOfCrafts: 1},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerEnchantingInput, uiEnchantingInput)},
		&v2193.ConsumeStackRequestAction{Count: 3, Source: s.slot(v2193.ContainerEnchantingMaterial, uiEnchantingLapis)},
		&v2193.PlaceStackRequestAction{Count: 1, Source: created(), Destination: s.slot(inv, 7)})
	s.closeContainer("the client closes the enchanting table")

	// The grindstone repairs two wooden swords into one.
	grindstone := cube.Pos{0, -60, 2}
	s.place("a grindstone is placed", grindstone, block.Grindstone{})
	s.clickBlock("the client opens the grindstone", grindstone)
	s.request("two damaged swords put in it", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 4), Destination: s.slot(v2193.ContainerGrindstoneInput, uiGrindstoneFirst)},
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 5), Destination: s.slot(v2193.ContainerGrindstoneAdditional, uiGrindstoneSecond)})
	s.request("the repaired sword taken", nil,
		&v2193.CraftGrindstoneRecipeStackRequestAction{NumberOfCrafts: 1},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerGrindstoneInput, uiGrindstoneFirst)},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerGrindstoneAdditional, uiGrindstoneSecond)},
		&v2193.PlaceStackRequestAction{Count: 1, Source: created(), Destination: s.slot(inv, 4)})
	s.closeContainer("the client closes the grindstone")

	// The stonecutter cuts stone into stone bricks.
	stonecutter := cube.Pos{0, -60, -2}
	s.place("a stonecutter is placed", stonecutter, block.Stonecutter{})
	s.clickBlock("the client opens the stonecutter", stonecutter)
	s.request("stone put in it", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 6), Destination: s.slot(v2193.ContainerStonecutterInput, uiStonecutterInput)})
	s.request("stone bricks cut", nil,
		&v2193.CraftRecipeStackRequestAction{RecipeNetworkID: s.recipe("stonecutter", block.StoneBricks{}, "minecraft:stone\""), NumberOfCrafts: 1},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerStonecutterInput, uiStonecutterInput)},
		&v2193.PlaceStackRequestAction{Count: 1, Source: created(), Destination: s.slot(inv, 22)})
	s.closeContainer("the client closes the stonecutter")

	// The smithing table upgrades a diamond sword to netherite.
	smithing := cube.Pos{2, -60, 2}
	s.place("a smithing table is placed", smithing, block.SmithingTable{})
	s.clickBlock("the client opens the smithing table", smithing)
	s.request("template, sword and ingot put in it", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 9), Destination: s.slot(v2193.ContainerSmithingTableTemplate, uiSmithingTemplate)},
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 10), Destination: s.slot(v2193.ContainerSmithingTableInput, uiSmithingInput)},
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 11), Destination: s.slot(v2193.ContainerSmithingTableMaterial, uiSmithingMaterial)})
	s.request("the netherite sword taken", nil,
		&v2193.CraftRecipeStackRequestAction{RecipeNetworkID: s.smithingRecipe(item.Sword{Tier: item.ToolTierNetherite}), NumberOfCrafts: 1},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerSmithingTableTemplate, uiSmithingTemplate)},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerSmithingTableInput, uiSmithingInput)},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerSmithingTableMaterial, uiSmithingMaterial)},
		&v2193.PlaceStackRequestAction{Count: 1, Source: created(), Destination: s.slot(inv, 10)})
	s.closeContainer("the client closes the smithing table")

	// The loom puts a border on a banner.
	loom := cube.Pos{-2, -60, 2}
	s.place("a loom is placed", loom, block.Loom{})
	s.clickBlock("the client opens the loom", loom)
	s.request("banner and dye put in it", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 12), Destination: s.slot(v2193.ContainerLoomInput, uiLoomInput)},
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 13), Destination: s.slot(v2193.ContainerLoomDye, uiLoomDye)})
	s.request("the bordered banner taken", nil,
		&v2193.CraftLoomRecipeStackRequestAction{Pattern: "bo", TimesCrafted: 1},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerLoomInput, uiLoomInput)},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerLoomDye, uiLoomDye)},
		&v2193.PlaceStackRequestAction{Count: 1, Source: created(), Destination: s.slot(inv, 12)})
	s.closeContainer("the client closes the loom")

	// A beacon on a pyramid of iron, which it finds on a tick a multiple of
	// 80.
	beacon := cube.Pos{-2, -60, -2}
	s.step("a beacon is placed on a pyramid of iron", func(tx *world.Tx, _ *player.Player) {
		for x := -1; x <= 1; x++ {
			for z := -1; z <= 1; z++ {
				tx.SetBlock(beacon.Add(cube.Pos{x, -1, z}), block.Iron{}, nil)
			}
		}
		tx.SetBlock(beacon, block.Beacon{}, nil)
	})
	s.skip("the beacon finds its pyramid", 80)
	s.clickBlock("the client opens the beacon", beacon)
	s.request("an iron ingot paid for speed", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 14), Destination: s.slot(v2193.ContainerBeaconPayment, uiBeaconPayment)})
	s.request("the payment taken", nil,
		&v2193.BeaconPaymentStackRequestAction{PrimaryEffect: 1},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerBeaconPayment, uiBeaconPayment)})
	s.closeContainer("the client closes the beacon")

	// A villager that sells bread for an emerald.
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
			// The skin of a villager comes of its UUID, else random.
		}, world.EntitySpawnOpts{Position: p.Position().Add(mgl64.Vec3{0, 0, -3}), ID: villagerUUID, SpawnFinalized: true})
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
	s.request("an emerald put in", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 16), Destination: s.slot(v2193.ContainerTradeTwoIngredientOne, uiTradeInput)})
	s.request("bread bought", nil,
		&v2193.CraftRecipeStackRequestAction{RecipeNetworkID: 1, NumberOfCrafts: 1},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerTradeTwoIngredientOne, uiTradeInput)},
		&v2193.PlaceStackRequestAction{Count: 6, Source: created(), Destination: s.slot(inv, 23)})
	s.closeContainer("the client closes the trade screen")

	// A book written in and a sign written on.
	s.client("the client writes a page of the book",
		&v2193.BookEditPacket{InventorySlot: 15, ActionType: v2193.BookActionReplacePage, PageNumber: 0, Text: "§sFirst page\nof the book"})
	sign := cube.Pos{1, -60, -1}
	s.step("a sign is placed and opened", func(tx *world.Tx, p *player.Player) {
		tx.SetBlock(sign, block.Sign{Wood: block.OakWood(), Attach: block.StandingAttachment(cube.OrientationFromYaw(0))}, nil)
		p.OpenSign(sign, true)
	})
	s.client("the client writes on the sign", &v2193.BlockActorDataPacket{
		Position: v2193.BlockPos{int32(sign[0]), int32(sign[1]), int32(sign[2])},
		NBTData: map[string]any{
			"id": "Sign", "x": int32(sign[0]), "y": int32(sign[1]), "z": int32(sign[2]),
			"FrontText": map[string]any{"Text": "§mGolden\nsign"},
			"BackText":  map[string]any{"Text": ""},
		},
	})

	// In creative mode, which destroys items and does not wear the anvil
	// out at random.
	s.gameModeStep("game mode: creative", 1, func(_ *world.Tx, p *player.Player) {
		p.SetGameMode(world.GameModeCreative)
	})
	s.request("destroy: a stone", nil,
		&v2193.DestroyStackRequestAction{Count: 1, Source: s.slot(inv, 20)})
	anvil := cube.Pos{1, -60, 1}
	s.place("an anvil is placed", anvil, block.Anvil{Type: block.UndamagedAnvil()})
	s.clickBlock("the client opens the anvil", anvil)
	s.request("a damaged sword and a diamond put in it", nil,
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 3), Destination: s.slot(v2193.ContainerAnvilInput, uiAnvilInput)},
		&v2193.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, 18), Destination: s.slot(v2193.ContainerAnvilMaterial, uiAnvilMaterial)})
	s.request("the sword renamed and repaired", []string{"§bExcalibur"},
		&v2193.CraftRecipeOptionalStackRequestAction{RecipeNetworkID: 0, FilterStringIndex: 0},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerAnvilInput, uiAnvilInput)},
		&v2193.ConsumeStackRequestAction{Count: 1, Source: s.slot(v2193.ContainerAnvilMaterial, uiAnvilMaterial)},
		&v2193.PlaceStackRequestAction{Count: 1, Source: created(), Destination: s.slot(inv, 3)})
	s.closeContainer("the client closes the anvil")
}

// request sends an item stack request of the actions passed, with the
// filter strings passed, and fails the test unless the server accepted it.
func (s *scenario) request(heading string, filter []string, actions ...v2193.StackRequestAction) {
	s.t.Helper()
	id := s.nextRequest
	s.nextRequest -= 2
	before := len(s.responses)
	s.client(heading, &v2193.ItemStackRequestPacket{Requests: []v2193.ItemStackRequest{{
		RequestID: id, Actions: actions, FilterStrings: filter,
	}}})
	if len(s.responses) == before {
		s.t.Errorf("%s: the request was not answered", heading)
	} else if status := s.responses[len(s.responses)-1]; status != v2193.ItemStackResponseStatusOK {
		s.t.Errorf("%s: the request was refused with status %d", heading, status)
	}
}

// place sets a block next to the player.
func (s *scenario) place(heading string, pos cube.Pos, b world.Block) {
	s.t.Helper()
	s.step(heading, func(tx *world.Tx, _ *player.Player) { tx.SetBlock(pos, b, nil) })
}

// clickBlock sends what the client sends when the player clicks the top of
// a block with the empty hand.
func (s *scenario) clickBlock(heading string, pos cube.Pos) {
	s.t.Helper()
	s.client(heading, &v2193.InventoryTransactionPacket{TransactionData: &v2193.UseItemTransactionData{
		ActionType:       v2193.UseItemActionClickBlock,
		TriggerType:      v2193.TriggerTypePlayerInput,
		BlockPosition:    [3]int32{int32(pos[0]), int32(pos[1]), int32(pos[2])},
		BlockFace:        int32(cube.FaceUp),
		HotBarSlot:       emptyHand,
		Position:         mgl32.Vec3{0.5, -58.38, 0.5},
		ClickedPosition:  mgl32.Vec3{0.5, 1, 0.5},
		ClientPrediction: v2193.ClientPredictionSuccess,
	}})
}

// closeContainer sends what the client sends when the player closes the
// container opened last.
func (s *scenario) closeContainer(heading string) {
	s.t.Helper()
	s.client(heading, &v2193.ContainerClosePacket{WindowID: byte(s.window), ContainerType: s.windowType})
}

// skip advances the world n ticks and records what the client was sent in
// all of them under one heading.
func (s *scenario) skip(heading string, n int) {
	s.t.Helper()
	s.begin(heading, true)
	for range n {
		s.advance()
	}
	s.drain(true)
}

// runtimeID returns the runtime ID the client knows an entity by.
func (s *scenario) runtimeID(h *world.EntityHandle) uint64 {
	return h.RuntimeID()
}

// recipe returns the network ID of the recipe of a block that makes the
// item passed of an input whose description holds input.
func (s *scenario) recipe(block string, output world.Item, input string) uint32 {
	s.t.Helper()
	rid, _, _ := bedrockdata.ItemRuntimeID(output)
	for _, r := range s.crafting.ShapedRecipes {
		if r.Block == block && len(r.Output) > 0 && r.Output[0].NetworkID == rid && strings.Contains(inline(reflect.ValueOf(r.Input)), input) {
			return r.RecipeNetworkID
		}
	}
	for _, r := range s.crafting.ShapelessRecipes {
		if r.Block == block && len(r.Output) > 0 && r.Output[0].NetworkID == rid && strings.Contains(inline(reflect.ValueOf(r.Input)), input) {
			return r.RecipeNetworkID
		}
	}
	s.t.Fatalf("no %s recipe for %T of %s", block, output, input)
	return 0
}

// smithingRecipe returns the network ID of the smithing transform that makes
// the item passed.
func (s *scenario) smithingRecipe(output world.Item) uint32 {
	s.t.Helper()
	rid, _, _ := bedrockdata.ItemRuntimeID(output)
	for _, r := range s.crafting.SmithingTransformRecipes {
		if r.Result.NetworkID == rid {
			return r.RecipeNetworkID
		}
	}
	s.t.Fatalf("no smithing recipe for %T", output)
	return 0
}
