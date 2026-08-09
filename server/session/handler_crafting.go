package session

import (
	"fmt"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/creative"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"math"
	"slices"
)

const fireworkMultiRecipe = "00000000-0000-0000-0000-000000000002"

var multiRecipeOutputs = map[string]string{
	"442d85ed-8272-4543-a6f1-418f90ded05d": "minecraft:filled_map",
	"8b36268c-1829-483c-a0f1-993b7156a8f2": "minecraft:filled_map",
	"602234e4-cac1-4353-8bb7-b1ebff70024b": "minecraft:filled_map",
	"98c84b38-1085-46bd-b1ce-dd38c159e6cc": "minecraft:filled_map",
	"d392b075-4ba1-40ae-8789-af868d56f6ce": "minecraft:filled_map",
	"85939755-ba10-4d9d-a4cc-efb7a8e943c4": "minecraft:filled_map",
	"aecd2294-4b94-434b-8667-4499bb2c9327": "minecraft:filled_map",
	"d1ca6b84-338e-4f2f-9c6b-76cc8b4bd98d": "minecraft:written_book",
	"d81aaeaf-e172-4440-9225-868df030d27b": "minecraft:banner",
	"b5c5d105-75a2-4076-af2b-923ea2bf4bf0": "minecraft:banner",
}

type multiCraft struct {
	recipe        recipe.Multi
	input         []item.Stack
	times         int
	consumed      int
	resultCreated bool
}

// handleCraft handles the CraftRecipe request action.
func (h *ItemStackRequestHandler) handleCraft(a *protocol.CraftRecipeStackRequestAction, s *Session, tx *world.Tx) error {
	craft, ok := s.recipes[a.RecipeNetworkID]
	if !ok {
		// Try dynamic recipes if no static recipe matches
		return h.tryDynamicCraft(s, tx, int(a.NumberOfCrafts))
	}
	if multi, ok := craft.(recipe.Multi); ok {
		return h.beginMultiCraft(multi, int(a.NumberOfCrafts), s)
	}
	_, shaped := craft.(recipe.Shaped)
	_, shapeless := craft.(recipe.Shapeless)
	_, userDataShapeless := craft.(recipe.UserDataShapeless)
	if !shaped && !shapeless && !userDataShapeless {
		return fmt.Errorf("recipe with network id %v is not a shaped or shapeless recipe", a.RecipeNetworkID)
	}
	if craft.Block() != "crafting_table" {
		return fmt.Errorf("recipe with network id %v is not a crafting table recipe", a.RecipeNetworkID)
	}

	timesCrafted := int(a.NumberOfCrafts)
	if timesCrafted < 1 {
		return fmt.Errorf("times crafted must be at least 1")
	}

	size := s.craftingSize()
	offset := s.craftingOffset()
	consumed := make([]bool, size)
	consumedInputs := make([]item.Stack, 0, len(craft.Input()))
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
			has, _ := s.ui.Item(int(slot))
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
			st := has.Grow(-remove)
			h.setItemInSlot(protocol.StackRequestSlotInfo{
				Container: protocol.FullContainerName{ContainerID: protocol.ContainerCraftingInput},
				Slot:      byte(slot),
			}, st, s, tx)
			break
		}
		if !processed {
			if lockedErr != nil {
				return lockedErr
			}
			return fmt.Errorf("recipe %v: could not consume expected item: %v", a.RecipeNetworkID, expected)
		}
	}
	output, err := craftingResults(craft, consumedInputs, timesCrafted, s.br)
	if err != nil {
		return err
	}
	return h.createResults(s, tx, output...)
}

func (h *ItemStackRequestHandler) beginMultiCraft(craft recipe.Multi, timesCrafted int, s *Session) error {
	if craft.Block() != "crafting_table" {
		return fmt.Errorf("multi recipe is not a crafting table recipe")
	}
	if timesCrafted < 1 {
		return fmt.Errorf("times crafted must be at least 1")
	}
	if h.multiCraft != nil {
		return fmt.Errorf("another multi recipe is already being crafted")
	}
	size, offset := s.craftingSize(), s.craftingOffset()
	input := make([]item.Stack, size)
	for slot := uint32(0); slot < size; slot++ {
		input[slot], _ = s.ui.Item(int(offset + slot))
	}
	h.multiCraft = &multiCraft{recipe: craft, input: input, times: timesCrafted}
	return nil
}

func (h *ItemStackRequestHandler) handleMultiConsume(a *protocol.ConsumeStackRequestAction, s *Session, tx *world.Tx) error {
	if a.Source.Container.ContainerID != protocol.ContainerCraftingInput || a.Count == 0 {
		return fmt.Errorf("multi recipe consumed invalid crafting input")
	}
	if err := h.verifySlot(a.Source, s, tx); err != nil {
		return err
	}
	has, err := h.itemInSlot(a.Source, s, tx)
	if err != nil {
		return err
	}
	if err := ensureUnlockedForCrafting(has); err != nil {
		return err
	}
	if has.Count() < int(a.Count) {
		return fmt.Errorf("multi recipe tried to consume %v items from a stack of %v", a.Count, has.Count())
	}
	if err := h.setItemInSlot(a.Source, has.Grow(-int(a.Count)), s, tx); err != nil {
		return err
	}
	h.multiCraft.consumed += int(a.Count)
	return nil
}

func (h *ItemStackRequestHandler) handleMultiResult(a *protocol.CraftResultsDeprecatedStackRequestAction, s *Session, tx *world.Tx) error {
	craft := h.multiCraft
	if craft.resultCreated || int(a.TimesCrafted) != craft.times || len(a.ResultItems) != 1 {
		return fmt.Errorf("multi recipe supplied invalid result metadata")
	}
	result := a.ResultItems[0]
	if result.Count == 0 {
		return fmt.Errorf("multi recipe result count must be at least 1")
	}

	var source item.Stack
	for _, input := range craft.input {
		if input.Empty() {
			continue
		}
		name, meta := input.Item().EncodeItem()
		if name == result.Identifier && meta == int16(result.MetadataValue) {
			source = input
			break
		}
	}

	var output item.Stack
	if !source.Empty() {
		if expected, restricted := multiRecipeOutputs[craft.recipe.UUID().String()]; restricted && result.Identifier != expected {
			return fmt.Errorf("multi recipe result %q must be %q", result.Identifier, expected)
		}
		if int(result.Count) > source.MaxCount() {
			return fmt.Errorf("multi recipe result count %v exceeds maximum %v", result.Count, source.MaxCount())
		}
		output = source.Grow(int(result.Count) - source.Count())
		if craft.recipe.UUID().String() == "00000000-0000-0000-0000-000000000001" {
			var err error
			output, err = repairedMultiResult(craft.input, output)
			if err != nil {
				return err
			}
		}
	} else {
		if craft.recipe.UUID().String() != fireworkMultiRecipe || result.Identifier != "minecraft:firework_rocket" {
			return fmt.Errorf("multi recipe result %q is not derived from its server-side input", result.Identifier)
		}
		it, ok := world.ItemByName(result.Identifier, int16(result.MetadataValue))
		if !ok {
			return fmt.Errorf("multi recipe result item %q is not registered", result.Identifier)
		}
		if n, ok := it.(world.NBTer); ok {
			decoded, ok := world.DecodeNBT(n, result.NBTData, s.br).(world.Item)
			if !ok {
				return fmt.Errorf("multi recipe result item %q decoded to a non-item", result.Identifier)
			}
			it = decoded
		}
		output = item.NewStack(it, int(result.Count))
		if output.Count() > output.MaxCount() || output.Count() > 3*craft.times {
			return fmt.Errorf("multi recipe result count %v is invalid", result.Count)
		}
	}
	craft.resultCreated = true
	return h.createResults(s, tx, output)
}

func repairedMultiResult(input []item.Stack, output item.Stack) (item.Stack, error) {
	if output.Count() != 1 || output.MaxDurability() < 1 {
		return item.Stack{}, fmt.Errorf("repair multi recipe must produce one durable item")
	}
	durability, matching := 0, 0
	for _, stack := range input {
		if stack.Empty() {
			continue
		}
		name, meta := stack.Item().EncodeItem()
		outputName, outputMeta := output.Item().EncodeItem()
		if name != outputName || meta != outputMeta || stack.MaxDurability() < 1 || stack.Count() != 1 {
			return item.Stack{}, fmt.Errorf("repair multi recipe requires exactly two matching durable items")
		}
		durability += stack.Durability()
		matching++
	}
	if matching != 2 {
		return item.Stack{}, fmt.Errorf("repair multi recipe requires exactly two matching durable items")
	}
	return output.WithDurability(min(output.MaxDurability(), durability+output.MaxDurability()/20)), nil
}

// handleAutoCraft handles the AutoCraftRecipe request action.
func (h *ItemStackRequestHandler) handleAutoCraft(a *protocol.AutoCraftRecipeStackRequestAction, s *Session, tx *world.Tx) error {
	craft, ok := s.recipes[a.RecipeNetworkID]
	if !ok {
		// Try dynamic recipes if no static recipe matches
		return h.tryDynamicCraft(s, tx, int(a.NumberOfCrafts))
	}
	_, shaped := craft.(recipe.Shaped)
	_, shapeless := craft.(recipe.Shapeless)
	_, userDataShapeless := craft.(recipe.UserDataShapeless)
	if !shaped && !shapeless && !userDataShapeless {
		return fmt.Errorf("recipe with network id %v is not a shaped or shapeless recipe", a.RecipeNetworkID)
	}
	if craft.Block() != "crafting_table" {
		return fmt.Errorf("recipe with network id %v is not a crafting table recipe", a.RecipeNetworkID)
	}

	timesCrafted := int(a.NumberOfCrafts)
	if timesCrafted < 1 {
		return fmt.Errorf("times crafted must be at least 1")
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

	consumedInputs := make([]item.Stack, 0, len(flattenedInputs))
	for _, expected := range flattenedInputs {
		remaining := expected.Count() * timesCrafted
		var lockedErr error

		for id, inv := range map[byte]*inventory.Inventory{
			protocol.ContainerCraftingInput:              s.ui,
			protocol.ContainerCombinedHotBarAndInventory: s.inv,
		} {
			for slot, has := range inv.Slots() {
				if has.Empty() {
					// We don't have this item, skip it.
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

				removal := has.Count()
				if remaining < removal {
					removal = remaining
				}
				remaining -= removal
				consumedInputs = append(consumedInputs, has.Grow(removal-has.Count()))

				has = has.Grow(-removal)
				h.setItemInSlot(protocol.StackRequestSlotInfo{
					Container: protocol.FullContainerName{ContainerID: id},
					Slot:      byte(slot),
				}, has, s, tx)
				if remaining == 0 {
					// Consumed this item, so go to the next one.
					break
				}
			}
			if remaining == 0 {
				// Consumed this item, so go to the next one.
				break
			}
		}
		if remaining != 0 {
			if lockedErr != nil {
				return lockedErr
			}
			return fmt.Errorf("recipe %v: could not consume expected item: %v", a.RecipeNetworkID, expected)
		}
	}

	output, err := craftingResults(craft, consumedInputs, timesCrafted, s.br)
	if err != nil {
		return err
	}
	return h.createResults(s, tx, output...)
}

// handleCreativeCraft handles the CreativeCraft request action.
func (h *ItemStackRequestHandler) handleCreativeCraft(a *protocol.CraftCreativeStackRequestAction, s *Session, tx *world.Tx, c Controllable) error {
	if !c.GameMode().CreativeInventory() {
		return fmt.Errorf("can only craft creative items in gamemode creative/spectator")
	}
	items := creative.Items()
	if a.CreativeItemNetworkID == 0 {
		return fmt.Errorf("creative item network ID must be at least 1")
	}
	index := a.CreativeItemNetworkID - 1
	if index >= uint32(len(items)) {
		return fmt.Errorf("creative item with network ID %v does not exist", a.CreativeItemNetworkID)
	}
	it := items[int(index)].Stack
	it = it.Grow(it.MaxCount() - 1)
	return h.createResults(s, tx, it)
}

// craftingSize gets the crafting size based on the opened container ID.
func (s *Session) craftingSize() uint32 {
	if s.openedContainerID.Load() == 1 {
		return craftingGridSizeLarge
	}
	return craftingGridSizeSmall
}

// craftingOffset gets the crafting offset based on the opened container ID.
func (s *Session) craftingOffset() uint32 {
	if s.openedContainerID.Load() == 1 {
		return craftingGridLargeOffset
	}
	return craftingGridSmallOffset
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

// tryDynamicCraft attempts to match the items in the crafting grid with any registered dynamic recipes.
func (h *ItemStackRequestHandler) tryDynamicCraft(s *Session, tx *world.Tx, timesCrafted int) error {
	if timesCrafted < 1 {
		return fmt.Errorf("times crafted must be at least 1")
	}

	size := s.craftingSize()
	offset := s.craftingOffset()

	// Collect all items from the crafting grid
	input := make([]recipe.Item, size)
	for i := uint32(0); i < size; i++ {
		slot := offset + i
		it, _ := s.ui.Item(int(slot))
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
		for i := uint32(0); i < size; i++ {
			slot := offset + i
			it, _ := s.ui.Item(int(slot))
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
		for i := uint32(0); i < size; i++ {
			slot := offset + i
			it, _ := s.ui.Item(int(slot))
			if !it.Empty() {
				if err := ensureUnlockedForCrafting(it); err != nil {
					return err
				}
				// Consume one item from this slot per craft
				st := it.Grow(-1 * timesCrafted)
				h.setItemInSlot(protocol.StackRequestSlotInfo{
					Container: protocol.FullContainerName{ContainerID: protocol.ContainerCraftingInput},
					Slot:      byte(slot),
				}, st, s, tx)
			}
		}

		return h.createResults(s, tx, repeatStacks(output, timesCrafted)...)
	}

	return fmt.Errorf("no matching recipe found for crafting grid")
}
