package session_test

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// anvilPos is where the cases of the anvil place it.
var anvilPos = cube.Pos{1, -60, 1}

// undamagedAnvil is the anvil the cases place, and set back after each
// request that may wear it.
var undamagedAnvil = block.Anvil{Type: block.UndamagedAnvil()}

// repair returns the actions of a request that takes the result of the
// anvil, renamed to the filter string at index if any, and puts it in a
// slot of the inventory.
func (s *scenario) repair(index int32, to int) []mc.StackRequestAction {
	return []mc.StackRequestAction{
		&mc.CraftRecipeOptionalStackRequestAction{RecipeNetworkID: 0, FilterStringIndex: index},
		&mc.ConsumeStackRequestAction{Count: 1, Source: s.slot(mc.ContainerAnvilInput, uiAnvilInput)},
		&mc.PlaceStackRequestAction{Count: 1, Source: s.created(), Destination: s.slot(mc.ContainerCombinedHotBarAndInventory, to)},
	}
}

// anvilInputs puts the stack of a slot of the inventory in the input slot
// of the anvil, and count items of another in the material slot, either
// left out if its slot is -1.
func (s *scenario) anvilInputs(heading string, input, material, count int) {
	s.t.Helper()
	const inv = mc.ContainerCombinedHotBarAndInventory
	var actions []mc.StackRequestAction
	if input >= 0 {
		actions = append(actions, &mc.PlaceStackRequestAction{Count: 1, Source: s.slot(inv, input), Destination: s.slot(mc.ContainerAnvilInput, uiAnvilInput)})
	}
	if material >= 0 {
		actions = append(actions, &mc.PlaceStackRequestAction{Count: byte(count), Source: s.slot(inv, material), Destination: s.slot(mc.ContainerAnvilMaterial, uiAnvilMaterial)})
	}
	s.request(heading, nil, actions...)
}

// anvilBack takes the stack of the input slot of the anvil and count items
// of its material slot back to two slots of the inventory, either left out
// if its slot is -1.
func (s *scenario) anvilBack(heading string, input, material, count int) {
	s.t.Helper()
	const inv = mc.ContainerCombinedHotBarAndInventory
	var actions []mc.StackRequestAction
	if input >= 0 {
		actions = append(actions, &mc.TakeStackRequestAction{Count: 1, Source: s.slot(mc.ContainerAnvilInput, uiAnvilInput), Destination: s.slot(inv, input)})
	}
	if material >= 0 {
		actions = append(actions, &mc.TakeStackRequestAction{Count: byte(count), Source: s.slot(mc.ContainerAnvilMaterial, uiAnvilMaterial), Destination: s.slot(inv, material)})
	}
	s.request(heading, nil, actions...)
}

// wornRequest sends a request of the anvil with the filter strings passed
// and fails the test unless the server accepted it if accept is set, or
// refused it if not. Outside creative a repair wears the anvil down a
// state 12% of the time, drawn from the global random source: the record
// of the step leaves out the UpdateBlock of the anvil, which must have
// been sent if and only if the anvil wore, and the anvil is then set back.
func (s *scenario) wornRequest(heading string, filter []string, accept bool, actions ...mc.StackRequestAction) {
	s.t.Helper()
	from := s.out.Len()
	id := s.nextRequest
	s.nextRequest -= 2
	before := len(s.responses)
	s.client(heading, &mc.ItemStackRequestPacket{Requests: []mc.ItemStackRequest{{
		RequestID: id, Actions: actions, FilterStrings: filter,
	}}})
	if len(s.responses) == before {
		s.t.Errorf("%s: the request was not answered", heading)
	} else if ok := s.responses[len(s.responses)-1] == mc.ItemStackResponseStatusOK; ok != accept {
		s.t.Errorf("%s: accepted %v, want %v", heading, ok, accept)
	}

	record := s.out.String()
	kept, dropped := dropUpdates(record[from:], anvilPos)
	s.out.Reset()
	s.out.WriteString(record[:from] + kept)
	var worn bool
	s.run(func(tx *world.Tx, _ *player.Player) { worn = tx.Block(anvilPos) != undamagedAnvil })
	if worn != (dropped > 0) {
		s.t.Errorf("%s: the anvil wore %v, but %d updates of its block were sent", heading, worn, dropped)
	}
	s.place("the anvil set back", anvilPos, undamagedAnvil)
}

// dropUpdates returns a part of a record without the UpdateBlock packets of
// the block at pos, and how many it left out.
func dropUpdates(record string, pos cube.Pos) (string, int) {
	lines := strings.SplitAfter(record, "\n")
	var b strings.Builder
	at := fmt.Sprintf("{Position:[%d %d %d] ", pos[0], pos[1], pos[2])
	dropped := 0
	for i := 0; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], ">> UpdateBlock ") && i+1 < len(lines) && strings.Contains(lines[i+1], at) {
			dropped++
			for i+1 < len(lines) && strings.HasPrefix(lines[i+1], "    ") {
				i++
			}
			continue
		}
		b.WriteString(lines[i])
	}
	return b.String(), dropped
}

// anvilCases are requests of the anvil in survival: refused for no input,
// inputs that are no repair, a repaired input, no work, a rename index out
// of range, a name too long or not UTF-8, a locked input or material,
// levels short and a cost of 40 or more; a repair with a material, with a
// second sword, an enchanted book merged, a rename capped at 39 levels,
// and a repair taken twice in a request, which fails after it paid; then
// in creative a cost over the limit, and a request once the anvil is gone
// from under the open menu.
func (s *scenario) anvilCases() {
	s.give("items given",
		slotStack{0, item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1).Damage(100)},
		slotStack{1, item.NewStack(item.IronIngot{}, 3)},
		slotStack{2, item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1)},
		slotStack{3, item.NewStack(item.Diamond{}, 1)},
		slotStack{4, item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1).Damage(50)},
		slotStack{5, item.NewStack(item.EnchantedBook{}, 1).WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 2))},
		slotStack{6, item.NewStack(item.Stick{}, 1)},
		slotStack{7, item.NewStack(block.Stone{}, 1)},
		slotStack{9, item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1).Damage(120)},
		slotStack{10, item.NewStack(item.Sword{Tier: item.ToolTierStone}, 1).WithAnvilCost(39)},
		slotStack{11, item.NewStack(item.EnchantedBook{}, 1).WithEnchantments(item.NewEnchantment(enchantment.Protection, 1))},
		slotStack{12, item.NewStack(item.Sword{Tier: item.ToolTierGold}, 1)},
		slotStack{13, item.NewStack(item.Sword{Tier: item.ToolTierWood}, 1)},
		slotStack{14, item.NewStack(item.EnchantedBook{}, 1).WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 2))},
	)
	s.step("the empty slot held and levels given", func(_ *world.Tx, p *player.Player) {
		if err := p.SetHeldSlot(emptyHand); err != nil {
			s.t.Error(err)
		}
		p.SetExperienceLevel(30)
	})
	s.place("an anvil is placed", anvilPos, undamagedAnvil)
	s.clickBlock("the client opens the anvil", anvilPos)

	s.wornRequest("refused: a repair with nothing in the anvil", nil, false, s.repair(0, 20)...)
	s.anvilInputs("a stick and stone put in it", 6, 7, 1)
	s.wornRequest("refused: the stick repaired with stone", nil, false, s.repair(0, 20)...)
	s.anvilBack("the stick and stone taken back", 6, 7, 1)
	s.anvilInputs("a whole diamond sword and a diamond put in it", 2, 3, 1)
	s.wornRequest("refused: the whole sword repaired", nil, false, s.repair(0, 20)...)
	s.anvilBack("the diamond taken back", -1, 3, 1)
	s.wornRequest("refused: the sword taken with no work", nil, false, s.repair(0, 20)...)
	s.wornRequest("refused: a rename of an index out of range", []string{"Blade"}, false, s.repair(1, 20)...)
	s.wornRequest("refused: a rename of 51 characters", []string{strings.Repeat("a", 51)}, false, s.repair(0, 20)...)
	s.wornRequest("refused: a rename not of UTF-8", []string{"bad \xff name"}, false, s.repair(0, 20)...)
	s.anvilInputs("a book of protection put in it", -1, 11, 1)
	s.wornRequest("refused: the sword merged with protection alone", nil, false, s.repair(0, 20)...)
	s.anvilBack("the sword and the book taken back", 2, 11, 1)

	// The client cannot put a locked item in the anvil.
	s.step("a locked sword in the anvil", func(_ *world.Tx, _ *player.Player) {
		_, _, _, ui, _ := s.sess.Inventories()
		_ = ui.SetItem(uiAnvilInput, item.NewStack(item.Sword{Tier: item.ToolTierGold}, 1).WithLockMode(item.LockInInventory))
	})
	s.wornRequest("refused: the locked sword renamed", []string{"Locked"}, false, s.repair(0, 20)...)
	s.step("the locked sword taken away, a sword and locked ingots in the anvil", func(_ *world.Tx, _ *player.Player) {
		_, _, _, ui, _ := s.sess.Inventories()
		_ = ui.SetItem(uiAnvilInput, item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1).Damage(10))
		_ = ui.SetItem(uiAnvilMaterial, item.NewStack(item.IronIngot{}, 1).WithLockMode(item.LockInSlot))
	})
	s.wornRequest("refused: the sword repaired with locked ingots", nil, false, s.repair(0, 20)...)
	s.step("the sword and ingots taken away", func(_ *world.Tx, _ *player.Player) {
		_, _, _, ui, _ := s.sess.Inventories()
		_ = ui.SetItem(uiAnvilInput, item.Stack{})
		_ = ui.SetItem(uiAnvilMaterial, item.Stack{})
	})

	s.anvilInputs("a damaged iron sword and three ingots put in it", 0, 1, 3)
	s.step("too few levels", func(_ *world.Tx, p *player.Player) { p.SetExperienceLevel(1) })
	s.wornRequest("refused: the sword repaired with a level", nil, false, s.repair(0, 20)...)
	s.step("levels given", func(_ *world.Tx, p *player.Player) { p.SetExperienceLevel(30) })
	s.wornRequest("the sword repaired and renamed with two ingots", []string{"Mended"}, true, s.repair(0, 20)...)
	s.anvilBack("the ingot left taken back", -1, 1, 1)

	s.anvilInputs("two damaged iron swords put in it", 4, 9, 1)
	s.wornRequest("one sword repaired with the other", nil, true, s.repair(0, 21)...)

	s.anvilInputs("a diamond sword and a book of sharpness put in it", 2, 5, 1)
	s.wornRequest("sharpness put on the sword", nil, true, s.repair(0, 22)...)

	// The stone sword was worked 39 levels before: a rename alone costs 39
	// rather than 40.
	s.anvilInputs("a stone sword worked before put in it", 10, -1, 0)
	s.step("38 levels", func(_ *world.Tx, p *player.Player) { p.SetExperienceLevel(38) })
	s.wornRequest("refused: the stone sword renamed with 38 levels", []string{"Worn"}, false, s.repair(0, 23)...)
	s.step("39 levels", func(_ *world.Tx, p *player.Player) { p.SetExperienceLevel(39) })
	s.wornRequest("the stone sword renamed with 39 levels", []string{"Worn"}, true, s.repair(0, 23)...)
	s.step("levels given", func(_ *world.Tx, p *player.Player) { p.SetExperienceLevel(30) })
	s.anvilInputs("the stone sword and a book of sharpness put in it", 23, 14, 1)
	s.wornRequest("refused: sharpness put on the stone sword for 40 levels or more", nil, false, s.repair(0, 24)...)
	s.anvilBack("the stone sword and the book taken back", 23, 14, 1)

	// The second rename takes the result of the first again: it is
	// refused after both paid and may have worn the anvil, and the first
	// is undone.
	s.anvilInputs("a golden sword put in it", 12, -1, 0)
	s.wornRequest("refused: two renames in a request", []string{"First"}, false,
		&mc.CraftRecipeOptionalStackRequestAction{RecipeNetworkID: 0, FilterStringIndex: 0},
		&mc.PlaceStackRequestAction{Count: 1, Source: s.slot(mc.ContainerCombinedHotBarAndInventory, 13), Destination: s.again(mc.ContainerAnvilInput, uiAnvilInput)},
		&mc.CraftRecipeOptionalStackRequestAction{RecipeNetworkID: 0, FilterStringIndex: 0},
	)
	s.anvilBack("the golden sword taken back", 12, -1, 0)

	s.gameModeStep("game mode: creative", 1, func(_ *world.Tx, p *player.Player) {
		p.SetGameMode(world.GameModeCreative)
	})
	s.anvilInputs("the stone sword and the book of sharpness put in it again", 23, 14, 1)
	s.request("sharpness put on the stone sword in creative", nil, s.repair(0, 24)...)
	s.anvilInputs("a wooden sword put in it", 13, -1, 0)
	s.step("the anvil broken under the open menu", func(tx *world.Tx, _ *player.Player) {
		tx.SetBlock(anvilPos, block.Stone{}, nil)
	})
	s.refuse("refused: the wooden sword renamed with no anvil",
		&mc.CraftRecipeOptionalStackRequestAction{RecipeNetworkID: 0, FilterStringIndex: 0})
	s.attempt("the wooden sword taken back",
		&mc.TakeStackRequestAction{Count: 1, Source: s.slot(mc.ContainerAnvilInput, uiAnvilInput), Destination: s.slot(mc.ContainerCombinedHotBarAndInventory, 13)})
	s.closeContainer("the client closes the anvil")
}
