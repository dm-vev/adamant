package session_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/creative"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// fuzzMenuPos is where FuzzMenuRequests places the block of its menu.
var fuzzMenuPos = cube.Pos{1, -60, 1}

// fuzzSlot is a slot of a container of item stack requests.
type fuzzSlot struct {
	container byte
	slot      int
}

// fuzzCommonSlots are the first 16 of the 24 slots the actions of
// FuzzMenuRequests name, whatever menu is open: slots of the inventory,
// also as the hot bar, the cursor, the off hand, the armour and the
// created output. The other 8 are of the menu.
var fuzzCommonSlots = []fuzzSlot{
	{v2193.ContainerCombinedHotBarAndInventory, 0},
	{v2193.ContainerCombinedHotBarAndInventory, 1},
	{v2193.ContainerCombinedHotBarAndInventory, 2},
	{v2193.ContainerCombinedHotBarAndInventory, 3},
	{v2193.ContainerCombinedHotBarAndInventory, 5},
	{v2193.ContainerCombinedHotBarAndInventory, 9},
	{v2193.ContainerCombinedHotBarAndInventory, 13},
	{v2193.ContainerCombinedHotBarAndInventory, 19},
	{v2193.ContainerCombinedHotBarAndInventory, 20},
	{v2193.ContainerCombinedHotBarAndInventory, 21},
	{v2193.ContainerHotBar, 1},
	{v2193.ContainerCursor, uiCursor},
	{v2193.ContainerOffhand, 1},
	{v2193.ContainerArmor, 0},
	{v2193.ContainerArmor, 1},
	{v2193.ContainerCreatedOutput, uiCreatedOutput},
}

// fuzzInventory returns the slots of the inventory of its own the slots of
// a menu are followed by, up to 8 slots in all.
func fuzzInventory(slots ...int) []fuzzSlot {
	out := make([]fuzzSlot, len(slots))
	for i, slot := range slots {
		out[i] = fuzzSlot{v2193.ContainerCombinedHotBarAndInventory, slot}
	}
	return out
}

// fuzzMenu is a menu FuzzMenuRequests opens: the block placed, if any, made
// anew for each run as a container holds its inventory, and what it holds,
// the slots of it the actions name, and whether the player is in creative
// mode whatever the input says.
type fuzzMenu struct {
	name     string
	block    func() world.Block
	fill     []slotStack
	slots    []fuzzSlot
	creative bool
}

// fuzzMenus are the menus of FuzzMenuRequests. The anvil is used in
// creative mode only, where it is not worn at random; the furnace has no
// experience to give, whose orbs fly off at random; and nothing enchanted
// goes in the grindstone, whose experience is drawn at random.
var fuzzMenus = []fuzzMenu{
	{name: "inventory", slots: append([]fuzzSlot{
		{v2193.ContainerCraftingInput, uiInventoryGrid}, {v2193.ContainerCraftingInput, uiInventoryGrid + 1},
		{v2193.ContainerCraftingInput, uiInventoryGrid + 2}, {v2193.ContainerCraftingInput, uiInventoryGrid + 3},
	}, fuzzInventory(4, 6, 7, 18)...)},
	{name: "crafting table", block: func() world.Block { return block.CraftingTable{} }, slots: append([]fuzzSlot{
		{v2193.ContainerCraftingInput, uiCraftingGrid}, {v2193.ContainerCraftingInput, uiCraftingGrid + 1},
		{v2193.ContainerCraftingInput, uiCraftingGrid + 3}, {v2193.ContainerCraftingInput, uiCraftingGrid + 4},
		{v2193.ContainerCraftingInput, uiCraftingGrid + 8},
	}, fuzzInventory(4, 6, 18)...)},
	{name: "chest", block: func() world.Block { return block.NewChest() }, fill: []slotStack{{0, item.NewStack(item.Apple{}, 4)}}, slots: append([]fuzzSlot{
		{v2193.ContainerLevelEntity, 0}, {v2193.ContainerLevelEntity, 1}, {v2193.ContainerLevelEntity, 26},
	}, fuzzInventory(4, 6, 7, 18, 16)...)},
	{name: "furnace", block: func() world.Block { return block.NewFurnace(cube.North) }, fill: []slotStack{{1, item.NewStack(item.Coal{}, 2)}, {2, item.NewStack(item.IronIngot{}, 3)}}, slots: append([]fuzzSlot{
		{v2193.ContainerFurnaceIngredient, 0}, {v2193.ContainerFurnaceFuel, 1}, {v2193.ContainerFurnaceResult, 2},
	}, fuzzInventory(15, 16, 4, 6, 18)...)},
	{name: "anvil", block: func() world.Block { return undamagedAnvil }, creative: true, slots: append([]fuzzSlot{
		{v2193.ContainerAnvilInput, uiAnvilInput}, {v2193.ContainerAnvilMaterial, uiAnvilMaterial},
	}, fuzzInventory(4, 13, 17, 11, 12, 18)...)},
	{name: "enchanting table", block: func() world.Block { return block.EnchantingTable{} }, slots: append([]fuzzSlot{
		{v2193.ContainerEnchantingInput, uiEnchantingInput}, {v2193.ContainerEnchantingMaterial, uiEnchantingLapis},
	}, fuzzInventory(17, 13, 4, 6, 18, 12)...)},
	{name: "grindstone", block: func() world.Block { return block.Grindstone{} }, slots: append([]fuzzSlot{
		{v2193.ContainerGrindstoneInput, uiGrindstoneFirst}, {v2193.ContainerGrindstoneAdditional, uiGrindstoneSecond},
	}, fuzzInventory(13, 17, 4, 6, 18, 12)...)},
	{name: "smithing table", block: func() world.Block { return block.SmithingTable{} }, slots: append([]fuzzSlot{
		{v2193.ContainerSmithingTableInput, uiSmithingInput}, {v2193.ContainerSmithingTableMaterial, uiSmithingMaterial},
		{v2193.ContainerSmithingTableTemplate, uiSmithingTemplate},
	}, fuzzInventory(11, 12, 13, 4, 6)...)},
	{name: "loom", block: func() world.Block { return block.Loom{} }, slots: append([]fuzzSlot{
		{v2193.ContainerLoomInput, uiLoomInput}, {v2193.ContainerLoomDye, uiLoomDye},
	}, fuzzInventory(7, 9, 4, 6, 18, 12)...)},
	{name: "stonecutter", block: func() world.Block { return block.Stonecutter{} }, slots: append([]fuzzSlot{
		{v2193.ContainerStonecutterInput, uiStonecutterInput},
	}, fuzzInventory(6, 4, 18, 1, 12, 16, 7)...)},
	{name: "beacon", block: func() world.Block { return block.Beacon{} }, slots: append([]fuzzSlot{
		{v2193.ContainerBeaconPayment, uiBeaconPayment},
	}, fuzzInventory(4, 6, 18, 1, 12, 16, 7)...)},
	{name: "cartography table", block: func() world.Block { return block.CartographyTable{} }, slots: append([]fuzzSlot{
		{v2193.ContainerCartographyInput, uiCartographyInput}, {v2193.ContainerCartographyAdditional, uiCartographyAddition},
	}, fuzzInventory(10, 4, 6, 18, 12, 16)...)},
}

// fuzzItems are what the inventory of the player holds as FuzzMenuRequests
// starts, by slot. Slot 8 is held, and empty.
var fuzzItems = []slotStack{
	{0, item.NewStack(item.Apple{}, 16)},
	{1, item.NewStack(block.Planks{Wood: block.OakWood()}, 8)},
	{2, item.NewStack(item.Pickaxe{Tier: item.ToolTierIron}, 1)},
	{3, item.NewStack(item.LapisLazuli{}, 10)},
	{4, item.NewStack(item.IronIngot{}, 8)},
	{5, item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1).Damage(100)},
	{6, item.NewStack(block.Stone{}, 8)},
	{7, item.NewStack(block.Banner{Colour: item.ColourWhite()}, 1)},
	{9, item.NewStack(item.Dye{Colour: item.ColourRed()}, 4)},
	{10, item.NewStack(item.Paper{}, 4)},
	{11, item.NewStack(item.NetheriteIngot{}, 1)},
	{12, item.NewStack(item.SmithingTemplate{Template: item.TemplateNetheriteUpgrade()}, 1)},
	{13, item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1)},
	{14, item.NewStack(item.Bundle{}, 1)},
	{15, item.NewStack(item.Coal{}, 4)},
	{16, item.NewStack(item.RawIron{}, 4)},
	{17, item.NewStack(item.Book{}, 2)},
	{18, item.NewStack(item.Stick{}, 4)},
	{19, item.NewStack(item.Apple{}, 2).WithLockMode(item.LockInSlot)},
}

// fuzzPatterns, fuzzEffects and fuzzResults are what the loom crafts, the
// beacon pays for and the special recipes name as their results, as the
// actions of FuzzMenuRequests pick them.
var (
	fuzzPatterns = []string{"bo", "cr", "", "xx"}
	fuzzEffects  = []int32{0, 1, 3, 11, 8, 5, 10, 6, 99}
	fuzzResults  = []item.Stack{
		item.NewStack(item.Stick{}, 4),
		item.NewStack(block.Banner{Colour: item.ColourWhite()}, 1),
		item.NewStack(item.Apple{}, 1),
	}
)

// fuzzAction is an action of an item stack request as FuzzMenuRequests
// decodes it from five bytes: its kind, with flags in the high bits, the
// slots it names of the 24, its count and the stack IDs it names them by.
type fuzzAction struct{ kind, src, dst, count, mode byte }

// The flags of the kind of a fuzzAction: it starts a request of its own,
// sent in a packet of its own, or in a PlayerAuthInput.
const (
	fuzzNewRequest = 0x80
	fuzzOwnPacket  = 0x40
	fuzzAuthInput  = 0x20
)

// fuzzRequest is a request of a fuzzMenuInput: its actions and how it is
// sent.
type fuzzRequest struct {
	ownPacket, authInput bool
	actions              []fuzzAction
}

// fuzzMenuInput is an input of FuzzMenuRequests: a menu, the game mode, and
// one to six actions in one or more requests.
type fuzzMenuInput struct {
	menu     fuzzMenu
	creative bool
	requests []fuzzRequest
}

// decodeFuzzMenuInput decodes an input of FuzzMenuRequests, and reports
// whether it holds at least one action.
func decodeFuzzMenuInput(b []byte) (fuzzMenuInput, bool) {
	if len(b) < 7 {
		return fuzzMenuInput{}, false
	}
	in := fuzzMenuInput{menu: fuzzMenus[int(b[0])%len(fuzzMenus)]}
	in.creative = b[1]&1 != 0 || in.menu.creative
	b = b[2:]
	for n := 0; n < 6 && len(b) >= 5; n++ {
		a := fuzzAction{b[0], b[1], b[2], b[3], b[4]}
		b = b[5:]
		if len(in.requests) == 0 || a.kind&fuzzNewRequest != 0 {
			auth := a.kind&fuzzAuthInput != 0
			in.requests = append(in.requests, fuzzRequest{ownPacket: auth || a.kind&fuzzOwnPacket != 0, authInput: auth})
		}
		r := &in.requests[len(in.requests)-1]
		r.actions = append(r.actions, a)
	}
	return in, true
}

// fuzzMenuSeeds are the seed corpus of FuzzMenuRequests: requests that the
// menus accept, and some they refuse, by menu.
func fuzzMenuSeeds() [][]byte {
	seed := func(menu, mode byte, actions ...fuzzAction) []byte {
		b := []byte{menu, mode}
		for _, a := range actions {
			b = append(b, a.kind, a.src, a.dst, a.count, a.mode)
		}
		return b
	}
	const (
		take, place, swap, drop, destroy, consume, create, craft, autoCraft, creativeCraft, optional, grind, loom, beacon, mine, results = 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15
		// The slots of fuzzCommonSlots, and of the menu from 16 on.
		apples, planks, lapis, sword, empty, cursor, offHand, created = 0, 1, 3, 4, 8, 11, 12, 15
		menu0, menu1, menu2, menu3, menu4, menu5                      = 16, 17, 18, 19, 20, 21
		// The stack IDs a mode names a slot by, the source's in the
		// low bits, the destination's above.
		known, again = 0, 5
	)
	return [][]byte{
		// The inventory screen: planks put in the grid, sticks crafted
		// and taken; apples put in the off hand, then a take refused for
		// a stack ID never sent.
		seed(0, 0, fuzzAction{place, planks, menu0, 1, known}, fuzzAction{place, planks, menu1, 1, again},
			fuzzAction{fuzzNewRequest | craft, 0, 0, 1, known}, fuzzAction{consume, menu0, 0, 1, known}, fuzzAction{consume, menu1, 0, 1, known},
			fuzzAction{place, created, empty, 4, again}),
		seed(0, 0, fuzzAction{place, apples, offHand, 2, known}, fuzzAction{fuzzNewRequest | fuzzOwnPacket | take, apples, cursor, 1, 7}),
		// The crafting table, through a PlayerAuthInput, and by the recipe
		// book.
		seed(1, 0, fuzzAction{fuzzAuthInput | place, planks, menu0, 2, known}, fuzzAction{fuzzNewRequest | autoCraft, 0, 0, 1, known},
			fuzzAction{consume, planks, 0, 2, known}, fuzzAction{place, created, empty, 4, again}),
		// The chest: a swap, a drop and a destroy in creative.
		seed(2, 1, fuzzAction{swap, apples, menu0, 0, known}, fuzzAction{drop, menu0, 0, 3, again},
			fuzzAction{fuzzNewRequest | destroy, sword, 0, 1, known}),
		// The furnace: the result taken.
		seed(3, 0, fuzzAction{take, menu2, empty, 2, known}, fuzzAction{place, menu3, menu1, 1, known}),
		// The anvil: the sword repaired with an ingot and renamed.
		seed(4, 1, fuzzAction{place, sword, menu0, 1, known}, fuzzAction{place, menu2, menu1, 1, known},
			fuzzAction{fuzzNewRequest | optional, 0, 0, 1, known}, fuzzAction{consume, menu0, 0, 1, known}, fuzzAction{consume, menu1, 0, 1, known},
			fuzzAction{place, created, empty, 1, again}),
		// The enchanting table: a book and lapis put in, an option taken.
		seed(5, 0, fuzzAction{place, menu2, menu0, 1, known}, fuzzAction{place, lapis, menu1, 3, known},
			fuzzAction{fuzzNewRequest | craft, 7, 0, 1, known}, fuzzAction{consume, menu0, 0, 1, known}, fuzzAction{consume, menu1, 0, 1, known},
			fuzzAction{place, created, empty, 1, again}),
		// The grindstone: the damaged sword ground.
		seed(6, 0, fuzzAction{place, sword, menu0, 1, known}, fuzzAction{fuzzNewRequest | grind, 0, 0, 1, known},
			fuzzAction{consume, menu0, 0, 1, known}, fuzzAction{place, created, empty, 1, again}),
		// The smithing table: the diamond sword, an ingot and the
		// template, the upgrade taken.
		seed(7, 0, fuzzAction{place, menu5, menu0, 1, known}, fuzzAction{place, menu3, menu1, 1, known}, fuzzAction{place, menu4, menu2, 1, known},
			fuzzAction{fuzzNewRequest | fuzzOwnPacket | craft, 2, 0, 1, known}, fuzzAction{consume, menu0, 0, 1, known}, fuzzAction{place, created, empty, 1, again}),
		// The loom: the banner and the dye, a border woven.
		seed(8, 0, fuzzAction{place, menu2, menu0, 1, known}, fuzzAction{place, menu3, menu1, 1, known},
			fuzzAction{fuzzNewRequest | loom, 0, 0, 1, known}, fuzzAction{consume, menu0, 0, 1, known}, fuzzAction{consume, menu1, 0, 1, known},
			fuzzAction{place, created, empty, 1, again}),
		// The stonecutter: stone cut into slabs.
		seed(9, 0, fuzzAction{place, menu1, menu0, 1, known}, fuzzAction{fuzzNewRequest | craft, 1, 0, 1, known},
			fuzzAction{consume, menu0, 0, 1, known}, fuzzAction{place, created, empty, 2, again}),
		// The beacon: an ingot paid for speed, and a destroy after.
		seed(10, 0, fuzzAction{place, menu1, menu0, 1, known}, fuzzAction{fuzzNewRequest | beacon, 1, 0, 0, known},
			fuzzAction{destroy, menu0, 0, 1, known}),
		// The cartography table: paper put in, a forged result.
		seed(11, 0, fuzzAction{place, menu2, menu0, 1, known}, fuzzAction{fuzzNewRequest | craft, 4, 0, 1, known},
			fuzzAction{results, 2, 0, 1, known}, fuzzAction{consume, menu0, 0, 1, known}),
		// A creative craft, mining with the held item, and a Create with
		// no craft.
		seed(0, 1, fuzzAction{creativeCraft, 0, 0, 1, known}, fuzzAction{place, created, empty, 64, again},
			fuzzAction{fuzzNewRequest | mine, 2, 0, 5, known}, fuzzAction{fuzzNewRequest | create, 0, 0, 0, known}),
	}
}

// FuzzMenuRequests sends the item stack requests of its input to a player
// in a menu, as the session handles them: through the batches of
// server/menu. Each request must be answered, once, and nothing may end the
// session, as a panic of a handler does. Until the handlers the session had
// were gone, it compared the two paths; now it has the one.
func FuzzMenuRequests(f *testing.F) {
	for _, b := range fuzzMenuSeeds() {
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		in, ok := decodeFuzzMenuInput(b)
		if !ok {
			return
		}
		runFuzzMenuInput(t, in)
	})
}

// runFuzzMenuInput runs an input of FuzzMenuRequests and fails the test
// unless the session answered each of its requests once.
func runFuzzMenuInput(t *testing.T, in fuzzMenuInput) {
	s := newScenario(t, "fuzz "+in.menu.name, scenarioConfig{})
	s.step("the inventory filled", func(tx *world.Tx, p *player.Player) {
		if err := p.SetHeldSlot(emptyHand); err != nil {
			t.Error(err)
		}
		for _, it := range fuzzItems {
			_ = p.Inventory().SetItem(it.slot, it.stack)
		}
		p.Armour().SetHelmet(item.NewStack(item.Helmet{Tier: item.ArmourTierLeather{}}, 1).WithEnchantments(item.NewEnchantment(enchantment.CurseOfBinding, 1)))
		p.SetExperienceLevel(30)
	})
	if in.creative {
		s.gameModeStep("game mode: creative", 1, func(_ *world.Tx, p *player.Player) { p.SetGameMode(world.GameModeCreative) })
	}
	if in.menu.block == nil {
		s.openInventory("the client opens its inventory")
	} else {
		b := in.menu.block()
		s.step("the "+in.menu.name+" placed", func(tx *world.Tx, _ *player.Player) {
			if _, ok := b.(block.Beacon); ok {
				for x := -1; x <= 1; x++ {
					for z := -1; z <= 1; z++ {
						tx.SetBlock(fuzzMenuPos.Add(cube.Pos{x, -1, z}), block.Iron{}, nil)
					}
				}
			}
			tx.SetBlock(fuzzMenuPos, b, nil)
			for _, it := range in.menu.fill {
				_ = tx.Block(fuzzMenuPos).(block.Container).Inventory(tx, fuzzMenuPos).SetItem(it.slot, it.stack)
			}
		})
		if _, ok := b.(block.Beacon); ok {
			s.skip("the beacon finds its pyramid", 80)
		}
		s.clickBlock("the client opens the "+in.menu.name, fuzzMenuPos)
	}
	before := len(s.responses)
	if sent, answered := s.sendFuzzRequests(in), len(s.responses)-before; answered != sent {
		t.Errorf("%s: %d requests sent, %d answered", in.menu.name, sent, answered)
	}

}

// sendFuzzRequests sends the requests of an input of FuzzMenuRequests, those
// not of a packet of their own in one packet with the requests before, and
// returns how many it sent.
func (s *scenario) sendFuzzRequests(in fuzzMenuInput) int {
	s.t.Helper()
	recipes := s.fuzzRecipes()
	slots := append(append([]fuzzSlot(nil), fuzzCommonSlots...), in.menu.slots...)
	var creativeIDs []uint32
	for _, it := range []world.Item{item.Apple{}, block.Stone{}} {
		creativeIDs = append(creativeIDs, creativeIndex(s, it)+1)
	}
	creativeIDs = append(creativeIDs, 0, uint32(len(creative.Items())+1))

	var pk *v2193.ItemStackRequestPacket
	flush := func() {
		if pk != nil {
			s.client("the requests", pk)
			pk = nil
		}
	}
	enchanted, sent := false, 0
	for _, r := range in.requests {
		if r.ownPacket {
			flush()
		}
		id := s.nextRequest
		s.nextRequest -= 2
		var actions []v2193.StackRequestAction
		for _, a := range r.actions {
			action := s.fuzzStackAction(a, id, slots, recipes, creativeIDs)
			if _, ok := action.(*v2193.CraftRecipeStackRequestAction); ok {
				if in.menu.name == "enchanting table" {
					// A second option would be of the seed drawn at
					// random after the first.
					if enchanted {
						continue
					}
					enchanted = true
				}
			}
			actions = append(actions, action)
		}
		req := v2193.ItemStackRequest{RequestID: id, Actions: actions, FilterStrings: []string{"Fuzz"}}
		sent++
		if r.authInput {
			s.client("a request in an input", &v2193.PlayerAuthInputPacket{
				Position:         [3]float32{0.5, -58.38, 0.5},
				InputData:        v2193.InputFlags{v2193.InputFlagPerformItemStackRequest},
				InputMode:        v2193.InputModeMouse,
				PlayMode:         v2193.PlayModeNormal,
				ItemStackRequest: proto.Some(req),
			})
			continue
		}
		if pk == nil {
			pk = &v2193.ItemStackRequestPacket{}
		}
		pk.Requests = append(pk.Requests, req)
	}
	flush()
	return sent
}

// fuzzRecipes are the recipes the crafts of FuzzMenuRequests name: sticks of
// planks, stone slabs of the stonecutter, the netherite sword of the
// smithing table, three special recipes, the options of the enchanting
// table and an ID of no recipe.
func (s *scenario) fuzzRecipes() []uint32 {
	return []uint32{
		s.recipe("crafting_table", item.Stick{}, "planks"),
		s.recipe("stonecutter", block.Slab{Block: block.Stone{}}, "minecraft:stone\""),
		s.smithingRecipe(item.Sword{Tier: item.ToolTierNetherite}),
		s.multiRecipe(multiRepair),
		s.multiRecipe(multiMapExtendCartograph),
		s.multiRecipe(multiBannerDuplicate),
		0, 1, 2, 60000,
	}
}

// fuzzStackAction returns the action of item stack request id that a
// fuzzAction is.
func (s *scenario) fuzzStackAction(a fuzzAction, id int32, slots []fuzzSlot, recipes, creativeIDs []uint32) v2193.StackRequestAction {
	src, dst := s.fuzzSlotInfo(slots, a.src, a.mode, id), s.fuzzSlotInfo(slots, a.dst, a.mode>>3, id)
	switch (a.kind & 0x1f) % 16 {
	case 0:
		return &v2193.TakeStackRequestAction{Count: a.count, Source: src, Destination: dst}
	case 1:
		return &v2193.PlaceStackRequestAction{Count: a.count, Source: src, Destination: dst}
	case 2:
		return &v2193.SwapStackRequestAction{Source: src, Destination: dst}
	case 3:
		return &v2193.DropStackRequestAction{Count: a.count, Source: src, Randomly: a.mode&0x40 != 0}
	case 4:
		return &v2193.DestroyStackRequestAction{Count: a.count, Source: src}
	case 5:
		return &v2193.ConsumeStackRequestAction{Count: a.count, Source: src}
	case 6:
		return &v2193.CreateStackRequestAction{ResultsSlot: a.count % 3}
	case 7:
		return &v2193.CraftRecipeStackRequestAction{RecipeNetworkID: recipes[int(a.src)%len(recipes)], NumberOfCrafts: a.count % 4}
	case 8:
		return &v2193.AutoCraftRecipeStackRequestAction{RecipeNetworkID: recipes[int(a.src)%len(recipes)], NumberOfCrafts: a.count % 4}
	case 9:
		return &v2193.CraftCreativeStackRequestAction{CreativeItemNetworkID: creativeIDs[int(a.src)%len(creativeIDs)], NumberOfCrafts: a.count % 3}
	case 10:
		return &v2193.CraftRecipeOptionalStackRequestAction{RecipeNetworkID: recipes[int(a.src)%len(recipes)], FilterStringIndex: int32(a.count%3) - 1}
	case 11:
		return &v2193.CraftGrindstoneRecipeStackRequestAction{RecipeNetworkID: recipes[int(a.src)%len(recipes)], NumberOfCrafts: a.count % 3}
	case 12:
		return &v2193.CraftLoomRecipeStackRequestAction{Pattern: fuzzPatterns[int(a.src)%len(fuzzPatterns)], TimesCrafted: a.count % 3}
	case 13:
		return &v2193.BeaconPaymentStackRequestAction{
			PrimaryEffect: fuzzEffects[int(a.src)%len(fuzzEffects)], SecondaryEffect: fuzzEffects[int(a.dst)%len(fuzzEffects)],
		}
	case 14:
		return &v2193.MineBlockStackRequestAction{HotbarSlot: int32(a.src % 9), PredictedDurability: int32(a.count), StackNetworkID: src.StackNetworkID}
	default:
		result := fuzzResults[int(a.src)%len(fuzzResults)]
		name, meta := result.Item().EncodeItem()
		return &v2193.CraftResultsDeprecatedStackRequestAction{TimesCrafted: a.count % 3, ResultItems: []v2193.StackRequestItem{{
			Identifier: name, MetadataValue: uint32(meta), Count: uint16(result.Count()),
		}}}
	}
}

// fuzzSlotInfo returns slot i of the slots passed, named by the stack ID
// the three low bits of mode pick: the ID the client was last sent for it
// (0 to 4), that of request id itself (5), that of the request before (6)
// or one it never was (7).
func (s *scenario) fuzzSlotInfo(slots []fuzzSlot, i, mode byte, id int32) v2193.StackRequestSlotInfo {
	sl := slots[int(i)%len(slots)]
	info := s.slot(sl.container, sl.slot)
	switch mode & 7 {
	case 5:
		info.StackNetworkID = id
	case 6:
		info.StackNetworkID = id + 2
	case 7:
		info.StackNetworkID = 9999
	}
	return info
}
