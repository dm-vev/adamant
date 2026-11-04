package menu

import (
	"fmt"
	"slices"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/recipe"
)

// AutoCraft is a recipe-book craft in progress. Its inputs are paid from
// the slots its caller names, in the crafting grid or the main inventory:
// BDS 1.26.52.3 takes exactly those slots and refuses a craft whose
// payments do not pay for all of its inputs.
type AutoCraft struct {
	t         *Tx
	recipe    recipe.Recipe
	times     int
	userData  bool
	inputs    []recipe.Item
	remaining []int
	taken     []item.Stack
}

// BeginAutoCraft starts a recipe-book craft of a shaped, shapeless or
// user-data shapeless recipe of the crafting table, crafted times times. A
// recipe the player has not unlocked is refused while limited_crafting is
// on. The inputs of the recipe that are the same item are owed together.
func (t *Tx) BeginAutoCraft(craft recipe.Recipe, timesCrafted int) (*AutoCraft, error) {
	if err := t.refused(); err != nil {
		return nil, err
	}
	_, shaped := craft.(recipe.Shaped)
	_, shapeless := craft.(recipe.Shapeless)
	_, userDataShapeless := craft.(recipe.UserDataShapeless)
	if !shaped && !shapeless && !userDataShapeless {
		return nil, fmt.Errorf("recipe is not a shaped or shapeless recipe")
	}
	if craft.Block() != "crafting_table" {
		return nil, fmt.Errorf("recipe is not a crafting table recipe")
	}
	if t.recipeLocked(craft) {
		return nil, fmt.Errorf("recipe is locked")
	}

	if timesCrafted < 1 {
		return nil, fmt.Errorf("times crafted must be at least 1")
	}

	flattenedInputs := make([]recipe.Item, 0, len(craft.Input()))
	for _, i := range craft.Input() {
		if i.Empty() {
			// We don't actually need this item - it's empty, so avoid putting it in our flattened inputs.
			continue
		}

		if ind := slices.IndexFunc(flattenedInputs, func(it recipe.Item) bool {
			return matchingCraftingStacks(it, i, userDataShapeless)
		}); ind >= 0 {
			flattenedInputs[ind] = grow(i, flattenedInputs[ind].Count())
			continue
		}
		flattenedInputs = append(flattenedInputs, i)
	}

	remaining := make([]int, len(flattenedInputs))
	for i, expected := range flattenedInputs {
		remaining[i] = expected.Count() * timesCrafted
	}
	return &AutoCraft{
		t:         t,
		recipe:    craft,
		times:     timesCrafted,
		userData:  userDataShapeless,
		inputs:    flattenedInputs,
		remaining: remaining,
	}, nil
}

// InGrid reports whether s is a slot of the crafting grid of the menu of
// the craft.
func (ac *AutoCraft) InGrid(s Slot) bool {
	offset, size := ac.t.o.Menu.Grid()
	return s.Inv == ac.t.o.Inv.UI && s.Index >= offset && s.Index < offset+size
}

// Pay takes count items of a slot towards the inputs still owed: each item
// must be owed, and none locked.
func (ac *AutoCraft) Pay(r Ref, count int) error {
	has, err := ac.t.Item(r.Slot)
	if err != nil {
		return err
	}
	if count < 1 || has.Count() < count {
		return fmt.Errorf("consumed %v items from a stack of %v", count, has.Count())
	}
	if err := ensureUnlockedForCrafting(has); err != nil {
		return err
	}
	left := count
	for i, expected := range ac.inputs {
		if left == 0 {
			break
		}
		if ac.remaining[i] == 0 || !matchingCraftingStacks(has, expected, ac.userData) {
			continue
		}
		n := min(left, ac.remaining[i])
		ac.remaining[i] -= n
		left -= n
	}
	if left != 0 {
		return fmt.Errorf("consumed %v not needed by the recipe", has.Grow(left-has.Count()))
	}
	ac.taken = append(ac.taken, has.Grow(count-has.Count()))
	return ac.t.set(r, has.Grow(-count))
}

// Finish ends the craft: it fails unless every input was paid, and makes
// the results of what was taken.
func (ac *AutoCraft) Finish() error {
	for i, expected := range ac.inputs {
		if ac.remaining[i] != 0 {
			return fmt.Errorf("could not consume expected item: %v", expected)
		}
	}
	output, err := craftingResults(ac.recipe, ac.taken, ac.times, ac.t.o.Tx.World().BlockRegistry())
	if err != nil {
		return err
	}
	return ac.t.createResults(output...)
}
