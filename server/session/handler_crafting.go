package session

import (
	"fmt"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/creative"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"math"
	"slices"
	"time"
)

const (
	repairMultiRecipe                  = "00000000-0000-0000-0000-000000000001"
	fireworkMultiRecipe                = "00000000-0000-0000-0000-000000000002"
	mapCloningCartographyMultiRecipe   = "442d85ed-8272-4543-a6f1-418f90ded05d"
	mapLockingMultiRecipe              = "602234e4-cac1-4353-8bb7-b1ebff70024b"
	mapCloningMultiRecipe              = "85939755-ba10-4d9d-a4cc-efb7a8e943c4"
	mapExtendingCartographyMultiRecipe = "8b36268c-1829-483c-a0f1-993b7156a8f2"
	mapUpgradingCartographyMultiRecipe = "98c84b38-1085-46bd-b1ce-dd38c159e6cc"
	mapUpgradingMultiRecipe            = "aecd2294-4b94-434b-8667-4499bb2c9327"
	bannerDuplicateMultiRecipe         = "b5c5d105-75a2-4076-af2b-923ea2bf4bf0"
	bookCloningMultiRecipe             = "d1ca6b84-338e-4f2f-9c6b-76cc8b4bd98d"
	mapExtendingMultiRecipe            = "d392b075-4ba1-40ae-8789-af868d56f6ce"
	bannerAddPatternMultiRecipe        = "d81aaeaf-e172-4440-9225-868df030d27b"
)

type multiCraft struct {
	recipe   recipe.Multi
	input    []item.Stack
	times    int
	consumed map[byte]int
	result   *protocol.StackRequestItem
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
	input := make([]item.Stack, size)
	for slot := uint32(0); slot < size; slot++ {
		input[slot], _ = s.ui.Item(int(offset + slot))
	}
	consumed := make([]bool, size)
	consumedInputs := make([]item.Stack, 0, len(craft.Input()))
	type removal struct {
		slot  uint32
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
			return fmt.Errorf("recipe %v: could not consume expected item: %v", a.RecipeNetworkID, expected)
		}
	}
	for slot, stack := range input {
		if !stack.Empty() && !consumed[slot] {
			return fmt.Errorf("recipe %v: crafting grid contains unexpected item: %v", a.RecipeNetworkID, stack)
		}
	}
	output, err := craftingResults(craft, consumedInputs, timesCrafted, s.br)
	if err != nil {
		return err
	}
	for _, removal := range removals {
		if err := h.setItemInSlot(protocol.StackRequestSlotInfo{
			Container: protocol.FullContainerName{ContainerID: protocol.ContainerCraftingInput},
			Slot:      byte(removal.slot),
		}, removal.stack, s, tx); err != nil {
			return err
		}
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
	h.multiCraft = &multiCraft{recipe: craft, input: input, times: timesCrafted, consumed: make(map[byte]int)}
	return nil
}

func (h *ItemStackRequestHandler) handleMultiConsume(a *protocol.ConsumeStackRequestAction, s *Session, tx *world.Tx) error {
	if a.Source.Container.ContainerID != protocol.ContainerCraftingInput || a.Count == 0 {
		return fmt.Errorf("multi recipe consumed invalid crafting input")
	}
	offset := int(s.craftingOffset())
	inputSlot := int(a.Source.Slot) - offset
	if inputSlot < 0 || inputSlot >= len(h.multiCraft.input) {
		return fmt.Errorf("multi recipe consumed slot outside the crafting grid")
	}
	if _, ok := h.multiCraft.consumed[a.Source.Slot]; ok {
		return fmt.Errorf("multi recipe consumed crafting slot %v more than once", a.Source.Slot)
	}
	if err := h.verifySlot(a.Source, s, tx); err != nil {
		return err
	}
	has, err := h.itemInSlot(a.Source, s, tx)
	if err != nil {
		return err
	}
	snapshot := h.multiCraft.input[inputSlot]
	if snapshot.Empty() || !snapshot.Equal(has) {
		return fmt.Errorf("multi recipe consumed an item not present in its crafting snapshot")
	}
	if err := ensureUnlockedForCrafting(snapshot); err != nil {
		return err
	}
	if snapshot.Count() < int(a.Count) {
		return fmt.Errorf("multi recipe tried to consume %v items from a stack of %v", a.Count, snapshot.Count())
	}
	h.multiCraft.consumed[a.Source.Slot] = int(a.Count)
	return nil
}

func (h *ItemStackRequestHandler) handleMultiResult(a *protocol.CraftResultsDeprecatedStackRequestAction, _ *Session, _ *world.Tx) error {
	craft := h.multiCraft
	if craft.result != nil || int(a.TimesCrafted) != craft.times || len(a.ResultItems) != 1 {
		return fmt.Errorf("multi recipe supplied invalid result metadata")
	}
	result := a.ResultItems[0]
	if result.Count == 0 {
		return fmt.Errorf("multi recipe result count must be at least 1")
	}
	craft.result = &result
	return nil
}

func (h *ItemStackRequestHandler) finishMultiCraft(s *Session, tx *world.Tx) error {
	craft := h.multiCraft
	if craft.result == nil || len(craft.consumed) == 0 {
		return fmt.Errorf("multi recipe requires one result and all consumed inputs")
	}
	for slot, snapshot := range craft.input {
		count, consumed := craft.consumed[byte(int(s.craftingOffset())+slot)]
		if snapshot.Empty() {
			if consumed {
				return fmt.Errorf("multi recipe consumed an empty crafting slot")
			}
			continue
		}
		if !consumed || count != craft.times || snapshot.Count() < count {
			return fmt.Errorf("multi recipe did not consume its exact crafting input")
		}
	}

	output, err := multiCraftResult(craft, s.br)
	if err != nil {
		return err
	}
	for slot, count := range craft.consumed {
		has, err := s.ui.Item(int(slot))
		if err != nil {
			return err
		}
		if err := h.setItemInSlot(protocol.StackRequestSlotInfo{
			Container: protocol.FullContainerName{ContainerID: protocol.ContainerCraftingInput},
			Slot:      slot,
		}, has.Grow(-count), s, tx); err != nil {
			return err
		}
	}
	return h.createResults(s, tx, output)
}

func multiCraftResult(craft *multiCraft, br world.BlockRegistry) (item.Stack, error) {
	result := *craft.result
	inputs := make([]item.Stack, 0, len(craft.input))
	for _, stack := range craft.input {
		if !stack.Empty() {
			inputs = append(inputs, stack)
		}
	}
	wantResult := func(source item.Stack, name string, count int) (item.Stack, error) {
		_, meta := source.Item().EncodeItem()
		if result.Identifier != name || result.MetadataValue > math.MaxInt16 || int16(result.MetadataValue) != meta || int(result.Count) != count || count > source.MaxCount() {
			return item.Stack{}, fmt.Errorf("multi recipe supplied invalid result %q x%v", result.Identifier, result.Count)
		}
		return source.Grow(count - source.Count()), nil
	}
	nameOf := func(stack item.Stack) string {
		name, _ := stack.Item().EncodeItem()
		return name
	}

	switch craft.recipe.UUID().String() {
	case repairMultiRecipe:
		if craft.times != 1 || len(inputs) != 2 {
			return item.Stack{}, fmt.Errorf("repair multi recipe requires exactly two inputs and one craft")
		}
		output, err := wantResult(inputs[0], nameOf(inputs[0]), 1)
		if err != nil {
			return item.Stack{}, err
		}
		return repairedMultiResult(inputs, output)
	case fireworkMultiRecipe:
		paper, gunpowder := 0, 0
		explosions := make([]item.FireworkExplosion, 0, len(inputs))
		for _, stack := range inputs {
			switch it := stack.Item().(type) {
			case item.Paper:
				paper++
			case item.Gunpowder:
				gunpowder++
			case item.FireworkStar:
				explosions = append(explosions, it.FireworkExplosion)
			default:
				return item.Stack{}, fmt.Errorf("firework multi recipe contains invalid input %v", stack)
			}
		}
		count := 3 * craft.times
		if paper != 1 || gunpowder < 1 || gunpowder > 3 || result.Identifier != "minecraft:firework_rocket" || result.MetadataValue != 0 || int(result.Count) != count || count > 64 {
			return item.Stack{}, fmt.Errorf("firework multi recipe supplied invalid inputs or result")
		}
		return item.NewStack(item.Firework{Duration: time.Duration(gunpowder+1) * 500 * time.Millisecond, Explosions: explosions}, count), nil
	case mapExtendingMultiRecipe, mapExtendingCartographyMultiRecipe, mapCloningMultiRecipe, mapCloningCartographyMultiRecipe,
		mapUpgradingMultiRecipe, mapUpgradingCartographyMultiRecipe, mapLockingMultiRecipe:
		var source item.Stack
		counts := map[string]int{}
		for _, stack := range inputs {
			name := nameOf(stack)
			counts[name]++
			if name == "minecraft:filled_map" {
				source = stack
			}
		}
		id, outputCount := craft.recipe.UUID().String(), craft.times
		valid := counts["minecraft:filled_map"] == 1
		switch id {
		case mapExtendingMultiRecipe:
			valid = valid && len(inputs) == 9 && counts["minecraft:paper"] == 8
		case mapExtendingCartographyMultiRecipe:
			valid = valid && len(inputs) == 2 && counts["minecraft:paper"] == 1
		case mapCloningMultiRecipe:
			valid = valid && counts["minecraft:empty_map"] >= 1 && len(inputs) == counts["minecraft:empty_map"]+1
			outputCount *= counts["minecraft:empty_map"] + 1
		case mapCloningCartographyMultiRecipe:
			valid = valid && len(inputs) == 2 && counts["minecraft:empty_map"] == 1
			outputCount *= 2
		case mapUpgradingMultiRecipe, mapUpgradingCartographyMultiRecipe:
			valid = valid && len(inputs) == 2 && counts["minecraft:compass"] == 1
		case mapLockingMultiRecipe:
			valid = valid && len(inputs) == 2 && counts["minecraft:glass_pane"] == 1
		}
		if !valid {
			return item.Stack{}, fmt.Errorf("map multi recipe contains invalid inputs")
		}
		return wantResult(source, "minecraft:filled_map", outputCount)
	case bookCloningMultiRecipe:
		var source item.Stack
		writable := 0
		for _, stack := range inputs {
			switch nameOf(stack) {
			case "minecraft:written_book":
				if !source.Empty() {
					return item.Stack{}, fmt.Errorf("book cloning requires one written book")
				}
				source = stack
			case "minecraft:writable_book":
				writable++
			default:
				return item.Stack{}, fmt.Errorf("book cloning contains invalid input")
			}
		}
		book, ok := source.Item().(item.WrittenBook)
		if !ok || writable == 0 || book.Generation.Uint8() > 1 {
			return item.Stack{}, fmt.Errorf("book cloning requires one cloneable written book and writable books")
		}
		if book.Generation.Uint8() == 0 {
			book.Generation = item.CopyGeneration()
		} else {
			book.Generation = item.CopyOfCopyGeneration()
		}
		return wantResult(source.WithItem(book), "minecraft:written_book", (writable+1)*craft.times)
	case bannerDuplicateMultiRecipe:
		if len(inputs) != 2 {
			return item.Stack{}, fmt.Errorf("banner duplication requires exactly two banners")
		}
		first, firstOK := inputs[0].Item().(block.Banner)
		second, secondOK := inputs[1].Item().(block.Banner)
		if !firstOK || !secondOK || first.Colour != second.Colour || (len(first.Patterns) == 0) == (len(second.Patterns) == 0) {
			return item.Stack{}, fmt.Errorf("banner duplication requires one patterned and one blank matching banner")
		}
		source := inputs[0]
		if len(second.Patterns) != 0 {
			source = inputs[1]
		}
		return wantResult(source, "minecraft:banner", 2*craft.times)
	case bannerAddPatternMultiRecipe:
		var source item.Stack
		var colour item.Colour
		dyes, patterns := 0, 0
		var pattern item.BannerPattern
		for _, stack := range inputs {
			switch it := stack.Item().(type) {
			case block.Banner:
				if !source.Empty() {
					return item.Stack{}, fmt.Errorf("banner pattern recipe contains multiple banners")
				}
				source = stack
			case item.Dye:
				if dyes > 0 && colour != it.Colour {
					return item.Stack{}, fmt.Errorf("banner pattern recipe contains different dyes")
				}
				colour, dyes = it.Colour, dyes+1
			case item.BannerPattern:
				pattern, patterns = it, patterns+1
			default:
				return item.Stack{}, fmt.Errorf("banner pattern recipe contains invalid input")
			}
		}
		sourceBanner, ok := source.Item().(block.Banner)
		if !ok || dyes == 0 || patterns > 1 || result.Identifier != "minecraft:banner" || int(result.Count) != craft.times {
			return item.Stack{}, fmt.Errorf("banner pattern recipe supplied invalid inputs or result")
		}
		decoded, ok := world.DecodeNBT(sourceBanner, result.NBTData, br).(block.Banner)
		if !ok || decoded.Colour != sourceBanner.Colour || len(decoded.Patterns) != len(sourceBanner.Patterns)+1 || !slices.Equal(decoded.Patterns[:len(sourceBanner.Patterns)], sourceBanner.Patterns) {
			return item.Stack{}, fmt.Errorf("banner pattern result does not extend its source banner")
		}
		added := decoded.Patterns[len(decoded.Patterns)-1]
		expectedPattern, hasPatternItem := added.Type.Item()
		if added.Colour != colour || (patterns == 1 && (!hasPatternItem || expectedPattern != pattern.Type)) {
			return item.Stack{}, fmt.Errorf("banner pattern result does not match its inputs")
		}
		return wantResult(source.WithItem(decoded), "minecraft:banner", craft.times)
	default:
		return item.Stack{}, fmt.Errorf("unsupported multi recipe UUID %v", craft.recipe.UUID())
	}
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
