package menu

import (
	"fmt"
	"math"
	"slices"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/world"
)

// Craft crafts a shaped, shapeless or user-data shapeless recipe of the
// crafting table times times from the crafting grid of the menu. Each input
// of the recipe takes the first slot of the grid that matches it and holds
// enough for every craft, and the grid must hold nothing else; a locked
// item is no input. A recipe the player has not unlocked is refused while
// limited_crafting is on. The output of a user-data shapeless recipe keeps
// the data of the input it is made of.
func (t *Tx) Craft(craft recipe.Recipe, timesCrafted int) error {
	_, shaped := craft.(recipe.Shaped)
	_, shapeless := craft.(recipe.Shapeless)
	_, userDataShapeless := craft.(recipe.UserDataShapeless)
	if !shaped && !shapeless && !userDataShapeless {
		return fmt.Errorf("recipe is not a shaped or shapeless recipe")
	}
	if craft.Block() != "crafting_table" {
		return fmt.Errorf("recipe is not a crafting table recipe")
	}
	if t.recipeLocked(craft) {
		return fmt.Errorf("recipe is locked")
	}

	if timesCrafted < 1 {
		return fmt.Errorf("times crafted must be at least 1")
	}

	offset, size := t.o.Menu.Grid()
	input := make([]item.Stack, size)
	for slot := 0; slot < size; slot++ {
		input[slot], _ = t.o.Inv.UI.Item(offset + slot)
	}
	consumed := make([]bool, size)
	consumedInputs := make([]item.Stack, 0, len(craft.Input()))
	type removal struct {
		slot  int
		stack item.Stack
	}
	removals := make([]removal, 0, len(craft.Input()))
	for _, expected := range craft.Input() {
		var (
			processed bool
			lockedErr error
		)
		for slot := offset; slot < offset+size; slot++ {
			if consumed[slot-offset] {
				// We've already consumed this slot, skip it.
				continue
			}
			has := input[slot-offset]
			if has.Empty() != expected.Empty() || has.Count() < expected.Count()*timesCrafted {
				// We can't process this item, as it's not a part of the recipe.
				continue
			}
			if !matchingCraftingStacks(has, expected, userDataShapeless) {
				// Not the same item.
				continue
			}
			if err := ensureUnlockedForCrafting(has); err != nil {
				lockedErr = err
				continue
			}
			processed, consumed[slot-offset] = true, true
			remove := expected.Count() * timesCrafted
			consumedInputs = append(consumedInputs, has.Grow(remove-has.Count()))
			removals = append(removals, removal{slot: slot, stack: has.Grow(-remove)})
			break
		}
		if !processed {
			if lockedErr != nil {
				return lockedErr
			}
			return fmt.Errorf("could not consume expected item: %v", expected)
		}
	}
	for slot, stack := range input {
		if !stack.Empty() && !consumed[slot] {
			return fmt.Errorf("crafting grid contains unexpected item: %v", stack)
		}
	}
	output, err := craftingResults(craft, consumedInputs, timesCrafted, t.o.Tx.World().BlockRegistry())
	if err != nil {
		return err
	}
	for _, removal := range removals {
		if err := t.set(t.named(Slot{Inv: t.o.Inv.UI, Index: removal.slot}, RoleCraftingGrid), removal.stack); err != nil {
			return err
		}
	}
	return t.createResults(output...)
}

// CraftDynamic crafts the dynamic recipe of the crafting table that
// matches the crafting grid of the menu, at most times times: as many as the
// smallest stack of the grid allows. Every stack of the grid is an input.
func (t *Tx) CraftDynamic(timesCrafted int) error {
	if timesCrafted < 1 {
		return fmt.Errorf("times crafted must be at least 1")
	}

	offset, size := t.o.Menu.Grid()

	// Collect all items from the crafting grid
	input := make([]recipe.Item, size)
	for i := 0; i < size; i++ {
		slot := offset + i
		it, _ := t.o.Inv.UI.Item(slot)
		if it.Empty() {
			input[i] = item.Stack{}
		} else {
			input[i] = it
		}
	}

	// Try to match with any dynamic recipe
	for _, dynamicRecipe := range recipe.DynamicRecipes() {
		if dynamicRecipe.Block() != "crafting_table" {
			continue
		}

		output, ok := dynamicRecipe.Match(input)
		if !ok {
			continue
		}

		// Found a matching dynamic recipe! Now validate ingredient counts and consume the items
		// For dynamic recipes, we consume all non-empty slots, but we need to ensure each slot
		// has enough items to craft timesCrafted times.
		minStackCount := math.MaxInt
		for i := 0; i < size; i++ {
			slot := offset + i
			it, _ := t.o.Inv.UI.Item(slot)
			if !it.Empty() {
				if it.Count() < minStackCount {
					minStackCount = it.Count()
				}
			}
		}

		// Cap timesCrafted to the minimum available stack count to prevent item duplication
		if minStackCount < timesCrafted {
			timesCrafted = minStackCount
		}

		// Now consume the validated amount from each non-empty slot
		for i := 0; i < size; i++ {
			slot := offset + i
			it, _ := t.o.Inv.UI.Item(slot)
			if !it.Empty() {
				if err := ensureUnlockedForCrafting(it); err != nil {
					return err
				}
				// Consume one item from this slot per craft
				st := it.Grow(-1 * timesCrafted)
				_ = t.set(t.named(Slot{Inv: t.o.Inv.UI, Index: slot}, RoleCraftingGrid), st)
			}
		}

		return t.createResults(repeatStacks(output, timesCrafted)...)
	}

	return fmt.Errorf("no matching recipe found for crafting grid")
}

// recipeLocked ports RecipeCraftingHolder.setRecipeUsed: with
// limited_crafting on, a player crafts a crafting recipe only once it is
// unlocked. Special recipes, which Bedrock serves as multi and dynamic
// recipes, are never locked.
func (t *Tx) recipeLocked(r recipe.Recipe) bool {
	if t.o.Tx == nil || !t.o.Tx.World().GameRuleBool("limited_crafting") {
		return false
	}
	unlocked := t.o.Owner.UnlockedRecipeIDs()
	for _, id := range recipe.JavaRecipeIDs(r) {
		if slices.Contains(unlocked, id) {
			return false
		}
	}
	return true
}

// matchingStacks returns true if the two stacks are the same in a crafting scenario.
func matchingStacks(has, expected recipe.Item) bool {
	switch expected := expected.(type) {
	case item.Stack:
		switch has := has.(type) {
		case recipe.ItemTag:
			name, _ := expected.Item().EncodeItem()
			return has.Contains(name)
		case item.Stack:
			_, variants := expected.Value("variants")
			if !variants {
				return has.Comparable(expected)
			}
			nameOne, _ := has.Item().EncodeItem()
			nameTwo, _ := expected.Item().EncodeItem()
			return nameOne == nameTwo
		}
		// Unknown recipe item type: treat as a mismatch to avoid panicking on unexpected data.
		return false
	case recipe.ItemTag:
		switch has := has.(type) {
		case item.Stack:
			name, _ := has.Item().EncodeItem()
			return expected.Contains(name)
		case recipe.ItemTag:
			return has.Tag() == expected.Tag()
		}
		// Unknown recipe item type: treat as a mismatch to avoid panicking on unexpected data.
		return false
	default:
		// Unknown recipe item type: treat as a mismatch to avoid panicking on unexpected data.
		return false
	}
}

// matchingCraftingStacks is matchingStacks, but for an input of a recipe
// that keeps user data, which matches a stack of the same item whatever
// its data.
func matchingCraftingStacks(has, expected recipe.Item, preserveUserData bool) bool {
	if !preserveUserData {
		return matchingStacks(has, expected)
	}
	hasStack, hasOK := has.(item.Stack)
	expectedStack, expectedOK := expected.(item.Stack)
	if !hasOK || !expectedOK {
		return matchingStacks(has, expected)
	}
	name, meta := hasStack.Item().EncodeItem()
	expectedName, expectedMeta := expectedStack.Item().EncodeItem()
	if _, variants := expectedStack.Value("variants"); variants {
		return name == expectedName
	}
	return name == expectedName && meta == expectedMeta
}

// craftingResults returns the results of crafting craft timesCrafted times
// of the inputs consumed. The output of a user-data shapeless recipe is
// decoded, with br, of the NBT of the input it is made of, one result for
// each such input.
func craftingResults(craft recipe.Recipe, consumed []item.Stack, timesCrafted int, br world.BlockRegistry) ([]item.Stack, error) {
	if _, ok := craft.(recipe.UserDataShapeless); !ok {
		return repeatStacks(craft.Output(), timesCrafted), nil
	}
	if len(craft.Output()) != 1 {
		return nil, fmt.Errorf("user-data shapeless recipe must have exactly one output")
	}
	output := craft.Output()[0]
	outputNBT, ok := output.Item().(world.NBTer)
	if !ok {
		return nil, fmt.Errorf("user-data shapeless recipe output %T cannot retain user data", output.Item())
	}

	results := make([]item.Stack, 0, timesCrafted)
	crafted := 0
	for _, expected := range craft.Input() {
		expectedStack, ok := expected.(item.Stack)
		if !ok || expectedStack.Count() < 1 {
			continue
		}
		if _, ok := expectedStack.Item().(world.NBTer); !ok {
			continue
		}
		for _, source := range consumed {
			if !matchingCraftingStacks(source, expected, true) || source.Count()%expectedStack.Count() != 0 {
				continue
			}
			sourceNBT, ok := source.Item().(world.NBTer)
			if !ok {
				continue
			}
			n := source.Count() / expectedStack.Count()
			decoded, ok := world.DecodeNBT(outputNBT, sourceNBT.EncodeNBT(), br).(world.Item)
			if !ok {
				return nil, fmt.Errorf("user-data shapeless recipe output %T decoded to a non-item", output.Item())
			}
			results = append(results, source.WithItem(decoded).Grow(output.Count()*n-source.Count()))
			crafted += n
		}
	}
	if crafted != timesCrafted {
		return nil, fmt.Errorf("user-data shapeless recipe retained data for %v crafts, want %v", crafted, timesCrafted)
	}
	return results, nil
}

// repeatStacks multiplies the count of all item stacks provided by the number of repetitions provided. Item
// stacks where the new count would exceed the item's max count are split into multiple item stacks.
func repeatStacks(items []item.Stack, repetitions int) []item.Stack {
	output := make([]item.Stack, 0, len(items))
	for _, o := range items {
		count, maxCount := o.Count(), o.MaxCount()
		total := count * repetitions

		stacks := int(math.Ceil(float64(total) / float64(maxCount)))
		for i := 0; i < stacks; i++ {
			inc := min(total, maxCount)
			total -= inc

			output = append(output, o.Grow(inc-count))
		}
	}
	return output
}

// grow returns i with count more items.
func grow(i recipe.Item, count int) recipe.Item {
	switch i := i.(type) {
	case item.Stack:
		return i.Grow(count)
	case recipe.ItemTag:
		return recipe.NewItemTag(i.Tag(), i.Count()+count)
	}
	// TODO: Support growing custom recipe item types if they are ever introduced.
	return i
}
