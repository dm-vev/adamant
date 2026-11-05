package session_test

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/protocol/bedrock/session"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/google/uuid"
)

var updateStacks = flag.Bool("update-stacks", false, "rewrite testdata/stack_requests.golden, the outcome of the item stack requests")

// stackStep is a request of a scenario of item stack requests. It is built when
// the step comes, as the network IDs of the stacks it names change with the
// steps before it.
type stackStep struct {
	label string
	build func(sc *session.StackScene) mc.ItemStackRequest
}

// stackScenario is item stack requests run on a session of a game mode: what
// it holds, the recipes it knows, the menu it has open, and the steps.
type stackScenario struct {
	name  string
	mode  world.GameMode
	setup func(sc *session.StackScene)
	steps []stackStep
}

// slot names a slot of a container as an action of a request does, by the
// network ID of the stack the client expects in it.
func slot(container uint8, index int, id int32) mc.StackRequestSlotInfo {
	return mc.StackRequestSlotInfo{Container: mc.FullContainerName{ContainerID: container}, Slot: byte(index), StackNetworkID: id}
}

// inSlot names a slot of the inventory of the player and the stack it holds.
func inSlot(sc *session.StackScene, index int) mc.StackRequestSlotInfo {
	it, _ := sc.Main().Item(index)
	return slot(mc.ContainerCombinedHotBarAndInventory, index, sc.StackID(it))
}

// step is a request of one action, or of the actions passed, by the ID passed.
func step(label string, id int32, actions func(sc *session.StackScene) []mc.StackRequestAction) stackStep {
	return stackStep{label, func(sc *session.StackScene) mc.ItemStackRequest {
		return mc.ItemStackRequest{RequestID: id, Actions: actions(sc)}
	}}
}

// single is a step of the one action passed.
func single(label string, id int32, action func(sc *session.StackScene) mc.StackRequestAction) stackStep {
	return step(label, id, func(sc *session.StackScene) []mc.StackRequestAction { return []mc.StackRequestAction{action(sc)} })
}

// stackScenarios returns the scenarios of item stack requests: for each type of
// action in stackActionTypes, requests that the handler accepts or refuses
// with an error of its own, and the requests that the menus' tests and the
// goldens of the menus run through the same handlers on top.
func stackScenarios() []stackScenario {
	const (
		inv    = mc.ContainerCombinedHotBarAndInventory
		grid   = mc.ContainerCraftingInput
		output = mc.ContainerCreatedOutput
	)
	stone := func(n int) item.Stack { return item.NewStack(block.Stone{}, n) }
	fill := func(sc *session.StackScene) {
		_ = sc.Main().SetItem(0, stone(10))
		_ = sc.Main().SetItem(1, item.NewStack(block.Dirt{}, 3))
		_ = sc.Main().SetItem(2, stone(60))
		_ = sc.Main().SetItem(4, item.NewStack(item.Pickaxe{Tier: item.ToolTierIron}, 1).WithDurability(100))
	}
	created := func(request int32) mc.StackRequestSlotInfo { return slot(output, menu.UIResult, request) }
	menus := []struct {
		name   string
		block  world.Block
		action mc.StackRequestAction
	}{
		{"beacon", block.Beacon{}, &mc.BeaconPaymentStackRequestAction{PrimaryEffect: 1}},
		{"loom", block.Loom{}, &mc.CraftLoomRecipeStackRequestAction{Pattern: "bs", TimesCrafted: 1}},
		{"grindstone", block.Grindstone{}, &mc.CraftGrindstoneRecipeStackRequestAction{}},
		{"anvil", block.Anvil{}, &mc.CraftRecipeOptionalStackRequestAction{}},
		{"smithing table", block.SmithingTable{}, &mc.CraftRecipeStackRequestAction{RecipeNetworkID: 9, NumberOfCrafts: 1}},
		{"stonecutter", block.Stonecutter{}, &mc.CraftRecipeStackRequestAction{RecipeNetworkID: 9, NumberOfCrafts: 1}},
		{"enchanting table", block.EnchantingTable{}, &mc.CraftRecipeStackRequestAction{RecipeNetworkID: 9, NumberOfCrafts: 1}},
		{"crafting table", block.CraftingTable{}, &mc.CraftRecipeStackRequestAction{RecipeNetworkID: 9, NumberOfCrafts: 1}},
	}

	scenarios := []stackScenario{
		{name: "moves", mode: world.GameModeSurvival, setup: fill, steps: []stackStep{
			single("take 4 of stone to an empty slot", -1, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.TakeStackRequestAction{Count: 4, Source: inSlot(sc, 0), Destination: slot(inv, 3, 0)}
			}),
			single("place 2 of the stone on the stone, the new stack named by the request", -2, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.PlaceStackRequestAction{Count: 2, Source: slot(inv, 3, -1), Destination: inSlot(sc, 2)}
			}),
			single("place a stack by an ID the slot does not hold", -3, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.PlaceStackRequestAction{Count: 1, Source: slot(inv, 0, 99999), Destination: slot(inv, 5, 0)}
			}),
			single("place from an unknown container", -4, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.PlaceStackRequestAction{Count: 1, Source: slot(250, 0, 0), Destination: slot(inv, 5, 0)}
			}),
			single("place a stack on its own slot", -5, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.PlaceStackRequestAction{Count: 1, Source: inSlot(sc, 0), Destination: inSlot(sc, 0)}
			}),
			single("take the result of a trade that is not open", -6, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.TakeStackRequestAction{Count: 1, Source: slot(mc.ContainerTradeResultPreview, 0, 0), Destination: slot(inv, 5, 0)}
			}),
			single("swap the dirt and the stone", -7, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.SwapStackRequestAction{Source: inSlot(sc, 1), Destination: inSlot(sc, 2)}
			}),
			single("swap with an empty slot", -8, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.SwapStackRequestAction{Source: inSlot(sc, 1), Destination: slot(inv, 7, 0)}
			}),
			single("swap an empty slot", -9, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.SwapStackRequestAction{Source: slot(inv, 7, 0), Destination: inSlot(sc, 1)}
			}),
			single("swap a slot with itself", -10, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.SwapStackRequestAction{Source: inSlot(sc, 1), Destination: inSlot(sc, 1)}
			}),
			single("drop 2 of the stone", -11, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.DropStackRequestAction{Count: 2, Source: inSlot(sc, 0)}
			}),
			single("drop from an empty slot", -12, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.DropStackRequestAction{Count: 2, Source: slot(inv, 9, 0)}
			}),
			single("destroy in survival", -13, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.DestroyStackRequestAction{Count: 1, Source: inSlot(sc, 0)}
			}),
			single("mine a block with the pickaxe", -14, func(sc *session.StackScene) mc.StackRequestAction {
				it, _ := sc.Main().Item(4)
				return &mc.MineBlockStackRequestAction{HotbarSlot: 4, PredictedDurability: 99, StackNetworkID: sc.StackID(it)}
			}),
			single("mine a block with a stack the slot does not hold", -15, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.MineBlockStackRequestAction{HotbarSlot: 4, PredictedDurability: 99, StackNetworkID: 99999}
			}),
			single("craft a creative item in survival", -16, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.CraftCreativeStackRequestAction{CreativeItemNetworkID: 1, NumberOfCrafts: 1}
			}),
			single("take the result of a craft that did not come", -17, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.CreateStackRequestAction{ResultsSlot: 0}
			}),
			step("two actions, the second refused, the first undone", -18, func(sc *session.StackScene) []mc.StackRequestAction {
				return []mc.StackRequestAction{
					&mc.TakeStackRequestAction{Count: 1, Source: inSlot(sc, 0), Destination: slot(inv, 6, 0)},
					&mc.TakeStackRequestAction{Count: 1, Source: slot(inv, 6, 99999), Destination: slot(inv, 8, 0)},
				}
			}),
			single("an action the session does not handle: not implemented", -19, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.CraftNonImplementedStackRequestAction{}
			}),
			single("an action the session does not handle: reserved", -20, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.CraftReservedStackRequestAction{}
			}),
			single("an action the session does not handle: lab table", -21, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.LabTableCombineStackRequestAction{}
			}),
			single("no action at all", -22, func(sc *session.StackScene) mc.StackRequestAction { return nil }),
		}},
		{name: "creative", mode: world.GameModeCreative, setup: fill, steps: []stackStep{
			single("destroy 5 of the stone", -1, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.DestroyStackRequestAction{Count: 5, Source: inSlot(sc, 0)}
			}),
			single("destroy a stack the slot does not hold", -2, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.DestroyStackRequestAction{Count: 5, Source: slot(inv, 0, 99999)}
			}),
			single("destroy in an unknown container", -3, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.DestroyStackRequestAction{Count: 5, Source: slot(250, 0, 0)}
			}),
			step("craft the first creative item and take it", -4, func(sc *session.StackScene) []mc.StackRequestAction {
				return []mc.StackRequestAction{
					&mc.CraftCreativeStackRequestAction{CreativeItemNetworkID: 1, NumberOfCrafts: 1},
					&mc.PlaceStackRequestAction{Count: 1, Source: created(-4), Destination: slot(inv, 9, 0)},
				}
			}),
			single("craft the creative item 0", -5, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.CraftCreativeStackRequestAction{CreativeItemNetworkID: 0, NumberOfCrafts: 1}
			}),
			single("craft a creative item beyond the last", -6, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.CraftCreativeStackRequestAction{CreativeItemNetworkID: 1 << 30, NumberOfCrafts: 1}
			}),
			single("drop in creative", -7, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.DropStackRequestAction{Count: 1, Source: inSlot(sc, 1)}
			}),
		}},
	}

	sticks := recipe.NewShaped([]recipe.Item{recipe.NewItemTag("minecraft:planks", 1), recipe.NewItemTag("minecraft:planks", 1)}, item.NewStack(item.Stick{}, 4), recipe.Shape{1, 2}, "crafting_table")
	windCharges := recipe.NewShapeless([]recipe.Item{item.NewStack(item.BreezeRod{}, 1)}, item.NewStack(item.WindCharge{}, 4), "crafting_table")
	planks := func(n int) item.Stack { return item.NewStack(block.Planks{Wood: block.OakWood()}, n) }
	scenarios = append(scenarios,
		stackScenario{name: "crafts", mode: world.GameModeSurvival, setup: func(sc *session.StackScene) {
			sc.AddRecipe(1, windCharges)
			_ = sc.UI().SetItem(menu.UICraftingSmall, item.NewStack(item.BreezeRod{}, 3))
		}, steps: []stackStep{
			step("craft wind charges, pay the rod, take them", -1, func(sc *session.StackScene) []mc.StackRequestAction {
				rod, _ := sc.UI().Item(menu.UICraftingSmall)
				return []mc.StackRequestAction{
					&mc.CraftRecipeStackRequestAction{RecipeNetworkID: 1, NumberOfCrafts: 1},
					&mc.ConsumeStackRequestAction{Count: 1, Source: slot(grid, menu.UICraftingSmall, sc.StackID(rod))},
					&mc.PlaceStackRequestAction{Count: 4, Source: created(-1), Destination: slot(inv, 9, 0)},
				}
			}),
			step("craft twice and take the result by its slot", -2, func(sc *session.StackScene) []mc.StackRequestAction {
				return []mc.StackRequestAction{
					&mc.CraftRecipeStackRequestAction{RecipeNetworkID: 1, NumberOfCrafts: 2},
					&mc.CreateStackRequestAction{ResultsSlot: 0},
					&mc.PlaceStackRequestAction{Count: 4, Source: created(-2), Destination: slot(inv, 10, 0)},
				}
			}),
			single("craft a recipe the session does not know", -3, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.CraftRecipeStackRequestAction{RecipeNetworkID: 77, NumberOfCrafts: 1}
			}),
			single("take the result of a craft that did not come", -4, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.CreateStackRequestAction{ResultsSlot: 3}
			}),
			single("a payment and a result of nothing in progress", -5, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.ConsumeStackRequestAction{Count: 1, Source: slot(grid, menu.UICraftingSmall, 0)}
			}),
			single("a result of nothing in progress", -6, func(sc *session.StackScene) mc.StackRequestAction {
				return &mc.CraftResultsDeprecatedStackRequestAction{TimesCrafted: 1}
			}),
		}},
		stackScenario{name: "recipe book in the inventory", mode: world.GameModeSurvival, setup: func(sc *session.StackScene) {
			sc.AddRecipe(1, sticks)
			_ = sc.Main().SetItem(0, planks(4))
			sc.OpenInventory()
		}, steps: []stackStep{
			step("sticks of the recipe book: craft, pay, name the result, take it", -1, func(sc *session.StackScene) []mc.StackRequestAction {
				return []mc.StackRequestAction{
					&mc.AutoCraftRecipeStackRequestAction{RecipeNetworkID: 1, NumberOfCrafts: 1},
					&mc.ConsumeStackRequestAction{Count: 2, Source: inSlot(sc, 0)},
					&mc.CraftResultsDeprecatedStackRequestAction{TimesCrafted: 1, ResultItems: []mc.StackRequestItem{{Identifier: "minecraft:stick", Count: 4}}},
					&mc.PlaceStackRequestAction{Count: 4, Source: created(-1), Destination: slot(inv, 9, 0)},
				}
			}),
			step("sticks again, the payment short", -2, func(sc *session.StackScene) []mc.StackRequestAction {
				return []mc.StackRequestAction{
					&mc.AutoCraftRecipeStackRequestAction{RecipeNetworkID: 1, NumberOfCrafts: 1},
					&mc.ConsumeStackRequestAction{Count: 1, Source: inSlot(sc, 0)},
				}
			}),
			step("sticks again, not paid at all", -3, func(sc *session.StackScene) []mc.StackRequestAction {
				return []mc.StackRequestAction{&mc.AutoCraftRecipeStackRequestAction{RecipeNetworkID: 1, NumberOfCrafts: 1}}
			}),
			step("sticks again, paid from a slot out of sync", -4, func(sc *session.StackScene) []mc.StackRequestAction {
				return []mc.StackRequestAction{
					&mc.AutoCraftRecipeStackRequestAction{RecipeNetworkID: 1, NumberOfCrafts: 1},
					&mc.ConsumeStackRequestAction{Count: 2, Source: slot(inv, 0, 99999)},
				}
			}),
			step("a recipe the session does not know", -5, func(sc *session.StackScene) []mc.StackRequestAction {
				return []mc.StackRequestAction{&mc.AutoCraftRecipeStackRequestAction{RecipeNetworkID: 77, NumberOfCrafts: 1}}
			}),
		}},
		stackScenario{name: "recipe book at a crafting table", mode: world.GameModeSurvival, setup: func(sc *session.StackScene) {
			sc.AddRecipe(1, sticks)
			_ = sc.Main().SetItem(0, planks(4))
			sc.OpenMenu(block.CraftingTable{})
			sc.OpenAs(uint32(mc.ContainerTypeWorkbench))
		}, steps: []stackStep{
			step("sticks of the recipe book: craft, pay, name the result, take it", -1, func(sc *session.StackScene) []mc.StackRequestAction {
				return []mc.StackRequestAction{
					&mc.AutoCraftRecipeStackRequestAction{RecipeNetworkID: 1, NumberOfCrafts: 1},
					&mc.ConsumeStackRequestAction{Count: 2, Source: inSlot(sc, 0)},
					&mc.CraftResultsDeprecatedStackRequestAction{TimesCrafted: 1, ResultItems: []mc.StackRequestItem{{Identifier: "minecraft:stick", Count: 4}}},
					&mc.PlaceStackRequestAction{Count: 4, Source: created(-1), Destination: slot(inv, 9, 0)},
				}
			}),
			step("sticks again, the payment short", -2, func(sc *session.StackScene) []mc.StackRequestAction {
				return []mc.StackRequestAction{
					&mc.AutoCraftRecipeStackRequestAction{RecipeNetworkID: 1, NumberOfCrafts: 1},
					&mc.ConsumeStackRequestAction{Count: 1, Source: inSlot(sc, 0)},
				}
			}),
			step("sticks again, not paid at all", -3, func(sc *session.StackScene) []mc.StackRequestAction {
				return []mc.StackRequestAction{&mc.AutoCraftRecipeStackRequestAction{RecipeNetworkID: 1, NumberOfCrafts: 1}}
			}),
			step("sticks again, paid from a slot out of sync", -4, func(sc *session.StackScene) []mc.StackRequestAction {
				return []mc.StackRequestAction{
					&mc.AutoCraftRecipeStackRequestAction{RecipeNetworkID: 1, NumberOfCrafts: 1},
					&mc.ConsumeStackRequestAction{Count: 2, Source: slot(inv, 0, 99999)},
				}
			}),
			step("a recipe the session does not know", -5, func(sc *session.StackScene) []mc.StackRequestAction {
				return []mc.StackRequestAction{&mc.AutoCraftRecipeStackRequestAction{RecipeNetworkID: 77, NumberOfCrafts: 1}}
			}),
		}},
		stackScenario{name: "special recipe", mode: world.GameModeSurvival, setup: func(sc *session.StackScene) {
			sc.AddRecipe(1, recipe.NewMulti(uuid.MustParse(session.RepairMultiRecipe)))
			_ = sc.UI().SetItem(menu.UICraftingSmall, item.NewStack(item.Bow{}, 1).WithDurability(10))
			_ = sc.UI().SetItem(menu.UICraftingSmall+1, item.NewStack(item.Bow{}, 1).WithDurability(20))
		}, steps: []stackStep{
			step("repair the two bows: craft, name the result, pay with both", -1, func(sc *session.StackScene) []mc.StackRequestAction {
				first, _ := sc.UI().Item(menu.UICraftingSmall)
				second, _ := sc.UI().Item(menu.UICraftingSmall + 1)
				result := item.NewStack(item.Bow{}, 1).WithDurability(49)
				return []mc.StackRequestAction{
					&mc.CraftRecipeStackRequestAction{RecipeNetworkID: 1, NumberOfCrafts: 1},
					&mc.CraftResultsDeprecatedStackRequestAction{TimesCrafted: 1, ResultItems: []mc.StackRequestItem{{Identifier: "minecraft:bow", Count: 1, NBTData: session.NetworkItemNBT(result)}}},
					&mc.ConsumeStackRequestAction{Count: 1, Source: slot(grid, menu.UICraftingSmall+1, sc.StackID(second))},
					&mc.ConsumeStackRequestAction{Count: 1, Source: slot(grid, menu.UICraftingSmall, sc.StackID(first))},
				}
			}),
			step("repair, and an action the craft does not take", -2, func(sc *session.StackScene) []mc.StackRequestAction {
				return []mc.StackRequestAction{
					&mc.CraftRecipeStackRequestAction{RecipeNetworkID: 1, NumberOfCrafts: 1},
					&mc.DropStackRequestAction{Count: 1, Source: slot(inv, 0, 0)},
				}
			}),
			step("repair, and nothing to say what it makes", -3, func(sc *session.StackScene) []mc.StackRequestAction {
				return []mc.StackRequestAction{&mc.CraftRecipeStackRequestAction{RecipeNetworkID: 1, NumberOfCrafts: 1}}
			}),
		}},
	)
	for _, m := range menus {
		steps := []stackStep{single("the action of the menu", -1, func(sc *session.StackScene) mc.StackRequestAction { return m.action })}
		if _, anvil := m.block.(block.Anvil); anvil {
			steps = append(steps,
				stackStep{"the name chosen by a filter string, out of range", func(sc *session.StackScene) mc.ItemStackRequest {
					return mc.ItemStackRequest{RequestID: -2, FilterStrings: []string{"a name"}, Actions: []mc.StackRequestAction{&mc.CraftRecipeOptionalStackRequestAction{FilterStringIndex: 3}}}
				}},
				stackStep{"the name chosen by a filter string", func(sc *session.StackScene) mc.ItemStackRequest {
					return mc.ItemStackRequest{RequestID: -3, FilterStrings: []string{"a name"}, Actions: []mc.StackRequestAction{&mc.CraftRecipeOptionalStackRequestAction{FilterStringIndex: 0}}}
				}})
		}
		steps = append(steps, single("a stone moved while the menu is open", -4, func(sc *session.StackScene) mc.StackRequestAction {
			return &mc.TakeStackRequestAction{Count: 1, Source: inSlot(sc, 0), Destination: slot(inv, 3, 0)}
		}))
		scenarios = append(scenarios, stackScenario{name: "open " + m.name, mode: world.GameModeSurvival, setup: func(sc *session.StackScene) {
			fill(sc)
			sc.OpenMenu(m.block)
		}, steps: steps})
	}
	return scenarios
}

// stackIDs numbers the network IDs of the stacks in the order the tests meet
// them, as the IDs themselves come from a counter the tests of the package
// share.
type stackIDs map[int32]int32

func (n stackIDs) of(id int32) int32 {
	if id <= 0 {
		return id
	}
	if _, ok := n[id]; !ok {
		n[id] = int32(len(n) + 1)
	}
	return n[id]
}

// stackIDInText matches a network ID in the text of an error.
var stackIDInText = regexp.MustCompile(`server had (\d+)`)

// inText numbers the network IDs in the text of an error.
func (n stackIDs) inText(text string) string {
	return stackIDInText.ReplaceAllStringFunc(text, func(m string) string {
		id, _ := strconv.Atoi(strings.TrimPrefix(m, "server had "))
		return fmt.Sprint("server had #", n.of(int32(id)))
	})
}

// numberStackResponse sorts the containers and slots of the item stack
// responses in the packets passed, which the handler writes from maps, and
// numbers the network IDs of the stacks in them.
func numberStackResponse(ids stackIDs, pks []proto.Packet) {
	for _, pk := range pks {
		r, ok := pk.(*mc.ItemStackResponsePacket)
		if !ok {
			continue
		}
		for i := range r.Responses {
			info := r.Responses[i].ContainerInfo
			slices.SortFunc(info, func(a, b mc.StackResponseContainerInfo) int {
				return int(a.Container.ContainerID) - int(b.Container.ContainerID)
			})
			for _, c := range info {
				slices.SortFunc(c.SlotInfo, func(a, b mc.StackResponseSlotInfo) int { return int(a.Slot) - int(b.Slot) })
				for j := range c.SlotInfo {
					c.SlotInfo[j].StackNetworkID = ids.of(c.SlotInfo[j].StackNetworkID)
				}
			}
		}
	}
}

// runStackScenario runs the scenario in a world of its own, by the table of the
// actions if tabled is set, and returns the lines of testdata/stack_requests.golden
// that tell what each step did: the error, the packets as each protocol is
// sent them, and what the inventories hold.
func runStackScenario(t *testing.T, c stackScenario, tabled bool) (lines []string) {
	t.Helper()
	world.DefaultBlockRegistry.Finalize()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer func() { _ = w.Close() }()
	if err := w.Do(func(tx *world.Tx) {
		lines = append(lines, "== "+c.name)
		sc := session.NewStackScene(tx, c.mode)
		c.setup(sc)
		ids := stackIDs{}
		for _, s := range c.steps {
			lines = append(lines, "-- "+s.label)
			res := func() (res session.StackResult) {
				defer func() {
					if r := recover(); r != nil {
						res = session.StackResult{Err: fmt.Sprint("panic: ", r)}
					}
				}()
				return sc.Request(s.build(sc), tabled)
			}()
			numberStackResponse(ids, res.Packets)
			lines = append(lines, "  error: "+ids.inText(res.Err))
			lines = append(lines, encodeTablePackets(res.Packets)...)
			for _, st := range sc.State() {
				lines = append(lines, fmt.Sprintf("  %s: %s id %d", st.Where, st.Stack, ids.of(st.ID)))
			}
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	return lines
}

// TestStackRequestActionsMatch runs the scenarios of stackScenarios, which reach
// every type of action of stackActionTypes, and compares what the session did
// with testdata/stack_requests.golden, which was recorded from the switch of
// ItemStackRequestHandler.handleRequest before the actions were dispatched by a
// table. Run with -update-stacks to write it again, and read the diff.
func TestStackRequestActionsMatch(t *testing.T) {
	var got []string
	for _, c := range stackScenarios() {
		got = append(got, runStackScenario(t, c, false)...)
	}
	checkTableGolden(t, filepath.Join("testdata", "stack_requests.golden"), *updateStacks, "stacks", got)
}

// TestStackRequestActionsTableMatchesSwitch runs the scenarios with the switch
// and with the table of the actions, and requires the same outcome.
func TestStackRequestActionsTableMatchesSwitch(t *testing.T) {
	for _, c := range stackScenarios() {
		t.Run(c.name, func(t *testing.T) {
			old, tabled := runStackScenario(t, c, false), runStackScenario(t, c, true)
			if strings.Join(old, "\n") != strings.Join(tabled, "\n") {
				for i := 0; i < min(len(old), len(tabled)); i++ {
					if old[i] != tabled[i] {
						t.Fatalf("line %d differs:\nswitch: %s\ntable:  %s", i+1, old[i], tabled[i])
					}
				}
				t.Fatalf("%d lines by the switch, %d by the table", len(old), len(tabled))
			}
		})
	}
}

// TestStackRequestScenariosReachEveryAction requires that the scenarios send an
// action of each type of stackActionTypes, so that a type added to the protocol
// is tried by someone.
func TestStackRequestScenariosReachEveryAction(t *testing.T) {
	want := map[string]bool{}
	for _, a := range stackActionTypes {
		want[fmt.Sprintf("%T", a)] = true
	}
	w := world.Config{Synchronous: true}.New()
	defer func() { _ = w.Close() }()
	if err := w.Do(func(tx *world.Tx) {
		for _, c := range stackScenarios() {
			sc := session.NewStackScene(tx, c.mode)
			c.setup(sc)
			for _, s := range c.steps {
				for _, a := range s.build(sc).Actions {
					delete(want, fmt.Sprintf("%T", a))
				}
			}
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	for name := range want {
		t.Errorf("no scenario sends a %s", name)
	}
}
