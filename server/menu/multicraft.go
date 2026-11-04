package menu

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/recipe"
)

// The UUIDs of the special recipes: Bedrock's multi recipes, which Java has
// as the special recipes of the crafting and cartography tables.
const (
	repairMultiRecipe                  = "00000000-0000-0000-0000-000000000001"
	fireworkMultiRecipe                = "00000000-0000-0000-0000-000000000002"
	mapCloningCartographyMultiRecipe   = "442d85ed-8272-4543-a6f1-418f90ded05d"
	mapLockingMultiRecipe              = "602234e4-cac1-4353-8bb7-b1ebff70024b"
	mapCloningMultiRecipe              = "85939755-ba10-4d9d-a4cc-efb7a8e943c4"
	mapExtendingCartographyMultiRecipe = "8b36268c-1829-483c-a0f1-993b7156a8f2"
	bannerDuplicateMultiRecipe         = "b5c5d105-75a2-4076-af2b-923ea2bf4bf0"
	bookCloningMultiRecipe             = "d1ca6b84-338e-4f2f-9c6b-76cc8b4bd98d"
	mapExtendingMultiRecipe            = "d392b075-4ba1-40ae-8789-af868d56f6ce"
	bannerAddPatternMultiRecipe        = "d81aaeaf-e172-4440-9225-868df030d27b"
)

// MultiCraft is a special recipe in progress: map cloning, extending and
// locking, banner duplication and patterns, book cloning, fireworks and
// the repair of two items. It holds a snapshot of its inputs, taken when it
// began, and the items its caller counts towards it.
type MultiCraft struct {
	t      *Tx
	recipe recipe.Multi
	// input is the snapshot of the input slots, the first of which is the
	// UI slot offset; roles names each for the slot it is.
	input  []item.Stack
	offset int
	roles  []Role
	times  int
	// consumed is the count of each UI slot counted towards the craft.
	consumed map[int]int
}

// BeginMultiCraft starts a special recipe crafted times times. It snapshots
// its inputs: the two inputs of a cartography table, of which only the map
// recipes of the table are crafted, or the crafting grid of the menu. A
// batch crafts one special recipe at a time.
func (t *Tx) BeginMultiCraft(craft recipe.Multi, timesCrafted int) (*MultiCraft, error) {
	if craft.Block() != "crafting_table" {
		return nil, fmt.Errorf("multi recipe is not a crafting table recipe")
	}
	if timesCrafted < 1 {
		return nil, fmt.Errorf("times crafted must be at least 1")
	}
	if t.multiCraft != nil {
		return nil, fmt.Errorf("another multi recipe is already being crafted")
	}
	if t.o.Menu != nil && t.o.Menu.Kind() == Cartography {
		switch craft.UUID().String() {
		case mapCloningCartographyMultiRecipe, mapExtendingCartographyMultiRecipe, mapLockingMultiRecipe:
		default:
			return nil, fmt.Errorf("multi recipe is not a cartography table recipe")
		}
		input := make([]item.Stack, 2)
		for slot := range input {
			input[slot], _ = t.o.Inv.UI.Item(UICartographyInput + slot)
		}
		t.multiCraft = &MultiCraft{t: t, recipe: craft, input: input, offset: UICartographyInput,
			roles: []Role{RoleCartographyInput, RoleCartographyAdditional},
			times: timesCrafted, consumed: make(map[int]int)}
		return t.multiCraft, nil
	}
	offset, size := t.o.Menu.Grid()
	input := make([]item.Stack, size)
	roles := make([]Role, size)
	for slot := 0; slot < size; slot++ {
		input[slot], _ = t.o.Inv.UI.Item(offset + slot)
		roles[slot] = RoleCraftingGrid
	}
	t.multiCraft = &MultiCraft{t: t, recipe: craft, input: input, offset: offset, roles: roles, times: timesCrafted, consumed: make(map[int]int)}
	return t.multiCraft, nil
}

// Inputs returns the slots of the snapshot of the craft, in order, named
// for their roles.
func (mc *MultiCraft) Inputs() []Ref {
	refs := make([]Ref, len(mc.input))
	for i := range mc.input {
		refs[i] = mc.t.named(Slot{Inv: mc.t.o.Inv.UI, Index: mc.offset + i}, mc.roles[i])
	}
	return refs
}

// Consume counts count items of an input slot towards the craft; it changes
// no slot yet. The slot must hold what the snapshot holds, not locked, and
// at least count items, and may be counted once.
func (mc *MultiCraft) Consume(r Ref, count int) error {
	inputSlot := r.Index - mc.offset
	if r.Inv != mc.t.o.Inv.UI || inputSlot < 0 || inputSlot >= len(mc.input) {
		return fmt.Errorf("multi recipe consumed slot outside the crafting grid")
	}
	if count < 1 {
		return fmt.Errorf("multi recipe consumed invalid crafting input")
	}
	if _, ok := mc.consumed[r.Index]; ok {
		return fmt.Errorf("multi recipe consumed crafting slot %v more than once", r.Index)
	}
	has, err := mc.t.Item(r.Slot)
	if err != nil {
		return err
	}
	snapshot := mc.input[inputSlot]
	if snapshot.Empty() || !snapshot.Equal(has) {
		return fmt.Errorf("multi recipe consumed an item not present in its crafting snapshot")
	}
	if err := ensureUnlockedForCrafting(snapshot); err != nil {
		return err
	}
	if snapshot.Count() < count {
		return fmt.Errorf("multi recipe tried to consume %v items from a stack of %v", count, snapshot.Count())
	}
	mc.consumed[r.Index] = count
	return nil
}

// Finish ends the craft. Every input of the snapshot must have been
// consumed, as many as the times crafted, and nothing else. It derives the
// result and, if check accepts it, scales or locks the map a map recipe
// changes, takes the consumed items in the order of their slots, and makes
// the result. check may be nil.
func (mc *MultiCraft) Finish(check func(result item.Stack) bool) error {
	t := mc.t
	t.multiCraft = nil
	if len(mc.consumed) == 0 {
		return fmt.Errorf("multi recipe requires one result and all consumed inputs")
	}
	for slot, snapshot := range mc.input {
		count, consumed := mc.consumed[mc.offset+slot]
		if snapshot.Empty() {
			if consumed {
				return fmt.Errorf("multi recipe consumed an empty crafting slot")
			}
			continue
		}
		if !consumed || count != mc.times || snapshot.Count() < count {
			return fmt.Errorf("multi recipe did not consume its exact crafting input")
		}
	}

	output, err := mc.result(check)
	if err != nil {
		return err
	}
	// The inputs are consumed in the order of their slots, whatever order the
	// client named them in: each write of the UI inventory sends all of it.
	for i := range mc.input {
		slot := mc.offset + i
		count, consumed := mc.consumed[slot]
		if !consumed {
			continue
		}
		has, err := t.o.Inv.UI.Item(slot)
		if err != nil {
			return err
		}
		if err := t.set(t.named(Slot{Inv: t.o.Inv.UI, Index: slot}, mc.roles[i]), has.Grow(-count)); err != nil {
			return err
		}
	}
	return t.createResults(output)
}

// result derives the result of the craft and, if check accepts it, changes
// the map of a map recipe that extends or locks one.
func (mc *MultiCraft) result(check func(result item.Stack) bool) (item.Stack, error) {
	output, err := deriveMultiCraftResult(mc)
	if err != nil {
		return item.Stack{}, err
	}
	if check != nil && !check(output) {
		return item.Stack{}, fmt.Errorf("multi recipe result does not match server-derived output")
	}
	switch mc.recipe.UUID().String() {
	case mapExtendingMultiRecipe, mapExtendingCartographyMultiRecipe, mapLockingMultiRecipe:
		for _, stack := range mc.input {
			if _, ok := stack.Item().(item.FilledMap); ok {
				return changedMapResult(stack, mc.recipe.UUID().String(), mc.t.o.Tx)
			}
		}
	}
	return output, nil
}
