package session_test

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// smithingPos is where the cases of the smithing table place it.
var smithingPos = cube.Pos{2, -60, 2}

// trimRecipe returns the network ID of the smithing trim recipe of the
// template passed.
func (s *scenario) trimRecipe(template world.Item) uint32 {
	s.t.Helper()
	name, _ := template.EncodeItem()
	for _, r := range s.crafting.SmithingTrimRecipes {
		if strings.Contains(inline(reflect.ValueOf(r.Template)), fmt.Sprintf("Name:%q ", name)) {
			return r.RecipeNetworkID
		}
	}
	s.t.Fatalf("no smithing trim recipe of %v", template)
	return 0
}

// smith returns the actions of a request that takes the result of a recipe
// of the smithing table and puts it in a slot of the inventory.
func (s *scenario) smith(id uint32, to int) []mc.StackRequestAction {
	return []mc.StackRequestAction{
		&mc.CraftRecipeStackRequestAction{RecipeNetworkID: id, NumberOfCrafts: 1},
		&mc.ConsumeStackRequestAction{Count: 1, Source: s.slot(mc.ContainerSmithingTableTemplate, uiSmithingTemplate)},
		&mc.ConsumeStackRequestAction{Count: 1, Source: s.slot(mc.ContainerSmithingTableInput, uiSmithingInput)},
		&mc.ConsumeStackRequestAction{Count: 1, Source: s.slot(mc.ContainerSmithingTableMaterial, uiSmithingMaterial)},
		&mc.PlaceStackRequestAction{Count: 1, Source: s.created(), Destination: s.slot(mc.ContainerCombinedHotBarAndInventory, to)},
	}
}

// smithingSlot is a slot of the smithing table and the container the
// client names it by.
type smithingSlot struct {
	container uint8
	slot      int
}

var (
	smithingTemplate = smithingSlot{mc.ContainerSmithingTableTemplate, uiSmithingTemplate}
	smithingInput    = smithingSlot{mc.ContainerSmithingTableInput, uiSmithingInput}
	smithingMaterial = smithingSlot{mc.ContainerSmithingTableMaterial, uiSmithingMaterial}
)

// smithingMove moves count items between a slot of the inventory and a
// slot of the smithing table, into the table if in is set.
func (s *scenario) smithingMove(heading string, in bool, moves ...struct {
	inv   int
	at    smithingSlot
	count int
}) {
	s.t.Helper()
	const inv = mc.ContainerCombinedHotBarAndInventory
	var actions []mc.StackRequestAction
	for _, m := range moves {
		if in {
			actions = append(actions, &mc.PlaceStackRequestAction{Count: byte(m.count), Source: s.slot(inv, m.inv), Destination: s.slot(m.at.container, m.at.slot)})
		} else {
			actions = append(actions, &mc.TakeStackRequestAction{Count: byte(m.count), Source: s.slot(m.at.container, m.at.slot), Destination: s.slot(inv, m.inv)})
		}
	}
	s.request(heading, nil, actions...)
}

// move is a move of smithingMove.
func move(inv int, at smithingSlot, count int) struct {
	inv   int
	at    smithingSlot
	count int
} {
	return struct {
		inv   int
		at    smithingSlot
		count int
	}{inv, at, count}
}

// smithingCases are requests of the smithing table: refused for a recipe
// that is none, one of another block, an input, material or template that
// is empty or does not match, and a locked input; a netherite upgrade and an armour
// trim; an upgrade taken twice in a request, which fails after it took its
// inputs; and a request once the table is gone from under the open menu.
func (s *scenario) smithingCases() {
	upgrade := item.NewStack(item.SmithingTemplate{Template: item.TemplateNetheriteUpgrade()}, 3)
	coast := item.SmithingTemplate{Template: item.TemplateCoast()}
	s.give("items given",
		slotStack{0, upgrade},
		slotStack{1, item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1).Damage(30).WithCustomName("Kept")},
		slotStack{2, item.NewStack(item.NetheriteIngot{}, 3)},
		slotStack{3, item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1)},
		slotStack{4, item.NewStack(item.Stick{}, 1)},
		slotStack{5, item.NewStack(coast, 1)},
		slotStack{6, item.NewStack(item.Chestplate{Tier: item.ArmourTierDiamond{}}, 1)},
		slotStack{7, item.NewStack(item.IronIngot{}, 1)},
		slotStack{9, item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1)},
		slotStack{10, item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1)},
	)
	s.step("the empty slot held", func(_ *world.Tx, p *player.Player) {
		if err := p.SetHeldSlot(emptyHand); err != nil {
			s.t.Error(err)
		}
	})
	s.place("a smithing table is placed", smithingPos, block.SmithingTable{})
	s.clickBlock("the client opens the smithing table", smithingPos)
	sword := s.smithingRecipe(item.Sword{Tier: item.ToolTierNetherite})
	trim := s.trimRecipe(coast)

	// An upgrade with a slot of the table empty, as BDS refuses it: 38
	// (MissingInputItem) for an empty input or material, 35 for the
	// template alone.
	var missing, noTemplate uint8 = mc.ItemStackResponseStatusMissingInputItem, mc.ItemStackResponseStatusMismatchedRecipeForInputGridItems
	s.refuseWith("refused: an upgrade with the table empty", missing, s.smith(sword, 20)...)
	s.smithingMove("the template put in", true, move(0, smithingTemplate, 1))
	s.refuseWith("refused: an upgrade of the template alone", missing, s.smith(sword, 20)...)
	s.smithingMove("a diamond sword put in", true, move(10, smithingInput, 1))
	s.refuseWith("refused: an upgrade with no material", missing, s.smith(sword, 20)...)
	s.smithingMove("the template taken back", false, move(0, smithingTemplate, 1))
	s.smithingMove("an ingot put in", true, move(2, smithingMaterial, 1))
	s.refuseWith("refused: an upgrade with no template", noTemplate, s.smith(sword, 20)...)
	s.refuseWith("refused: a trim with no template", noTemplate, s.smith(trim, 20)...)
	s.smithingMove("the sword and the ingot taken back", false, move(10, smithingInput, 1), move(2, smithingMaterial, 1))
	s.smithingMove("the template, an iron sword and an ingot put in it", true,
		move(0, smithingTemplate, 1), move(3, smithingInput, 1), move(2, smithingMaterial, 1))
	s.refuse("refused: a recipe that is none", s.smith(99999, 20)...)
	s.refuse("refused: a recipe of the crafting table", s.smith(s.recipe("crafting_table", item.Stick{}, "planks"), 20)...)
	s.refuse("refused: the iron sword upgraded", s.smith(sword, 20)...)
	s.smithingMove("the iron sword taken back, a diamond sword put in", false, move(3, smithingInput, 1))
	s.smithingMove("a diamond sword put in", true, move(1, smithingInput, 1))
	s.refuse("refused: the diamond sword trimmed", s.smith(trim, 20)...)
	s.smithingMove("the ingot taken back, a stick put in", false, move(2, smithingMaterial, 1))
	s.smithingMove("a stick put in", true, move(4, smithingMaterial, 1))
	s.refuse("refused: the diamond sword upgraded with a stick", s.smith(sword, 20)...)
	s.smithingMove("the stick and the template taken back", false, move(4, smithingMaterial, 1), move(0, smithingTemplate, 1))
	s.smithingMove("an ingot and the coast template put in", true, move(2, smithingMaterial, 1), move(5, smithingTemplate, 1))
	s.refuse("refused: the diamond sword upgraded with the coast template", s.smith(sword, 20)...)
	s.smithingMove("the coast template taken back", false, move(5, smithingTemplate, 1))
	s.smithingMove("the template put in", true, move(0, smithingTemplate, 1))
	s.step("the sword locked", func(_ *world.Tx, _ *player.Player) {
		_, _, _, ui, _ := s.sess.Inventories()
		it, _ := ui.Item(uiSmithingInput)
		_ = ui.SetItem(uiSmithingInput, it.WithLockMode(item.LockInSlot))
	})
	s.refuse("refused: the locked sword upgraded", s.smith(sword, 20)...)
	s.step("the sword unlocked", func(_ *world.Tx, _ *player.Player) {
		_, _, _, ui, _ := s.sess.Inventories()
		it, _ := ui.Item(uiSmithingInput)
		_ = ui.SetItem(uiSmithingInput, it.WithLockMode(item.NotLocked))
	})
	s.request("the diamond sword upgraded, its name and damage kept", nil, s.smith(sword, 20)...)

	// The second upgrade takes the result of the first again: it is
	// refused after it took its inputs, and the first is undone.
	s.smithingMove("two templates and two ingots put in", true, move(0, smithingTemplate, 2), move(2, smithingMaterial, 2))
	s.smithingMove("a diamond sword put in", true, move(9, smithingInput, 1))
	s.refuse("refused: two upgrades in a request",
		&mc.CraftRecipeStackRequestAction{RecipeNetworkID: sword, NumberOfCrafts: 1},
		&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(mc.ContainerCombinedHotBarAndInventory, 10), Destination: s.again(mc.ContainerSmithingTableInput, uiSmithingInput)},
		&mc.CraftRecipeStackRequestAction{RecipeNetworkID: sword, NumberOfCrafts: 1})
	s.smithingMove("the sword, the template and the ingot taken back", false,
		move(9, smithingInput, 1), move(0, smithingTemplate, 2), move(2, smithingMaterial, 2))

	s.smithingMove("the coast template, a chestplate and an iron ingot put in", true,
		move(5, smithingTemplate, 1), move(6, smithingInput, 1), move(7, smithingMaterial, 1))
	s.request("the chestplate trimmed", nil, s.smith(trim, 21)...)

	s.smithingMove("the template and a sword put in", true, move(0, smithingTemplate, 1), move(9, smithingInput, 1))
	s.step("the smithing table broken under the open menu", func(tx *world.Tx, _ *player.Player) {
		tx.SetBlock(smithingPos, block.Stone{}, nil)
	})
	s.refuse("refused: the sword upgraded with no table", &mc.CraftRecipeStackRequestAction{RecipeNetworkID: sword, NumberOfCrafts: 1})
	s.attempt("the sword and the template taken back",
		&mc.TakeStackRequestAction{Count: 1, Source: s.slot(mc.ContainerSmithingTableInput, uiSmithingInput), Destination: s.slot(mc.ContainerCombinedHotBarAndInventory, 9)},
		&mc.TakeStackRequestAction{Count: 1, Source: s.slot(mc.ContainerSmithingTableTemplate, uiSmithingTemplate), Destination: s.slot(mc.ContainerCombinedHotBarAndInventory, 0)})
	s.closeContainer("the client closes the smithing table")
}
