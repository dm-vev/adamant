package menu

import (
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// deriveMultiCraftResult derives the result of a special recipe of its
// snapshot, as the server sees it: the client's word on the result is
// checked against it, never taken.
func deriveMultiCraftResult(craft *MultiCraft) (item.Stack, error) {
	inputs := make([]item.Stack, 0, len(craft.input))
	for _, stack := range craft.input {
		if !stack.Empty() {
			inputs = append(inputs, stack)
		}
	}
	withCount := func(source item.Stack, count int) (item.Stack, error) {
		if count < 1 || count > source.MaxCount() {
			return item.Stack{}, fmt.Errorf("multi recipe output count %v is invalid", count)
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
		return repairedMultiResult(inputs)
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
		if paper != 1 || gunpowder < 1 || gunpowder > 3 || count > 64 {
			return item.Stack{}, fmt.Errorf("firework multi recipe supplied invalid inputs")
		}
		return item.NewStack(item.Firework{Duration: time.Duration(gunpowder+1) * 500 * time.Millisecond, Explosions: explosions}, count), nil
	case mapExtendingMultiRecipe, mapExtendingCartographyMultiRecipe, mapCloningMultiRecipe, mapCloningCartographyMultiRecipe, mapLockingMultiRecipe:
		var source item.Stack
		counts := map[string]int{}
		for _, stack := range inputs {
			name := nameOf(stack)
			counts[name]++
			if name == "minecraft:filled_map" {
				source = stack
			}
		}
		id, outputCount := craft.recipe.UUID().String(), 1
		valid := counts["minecraft:filled_map"] == 1
		switch id {
		case mapExtendingMultiRecipe:
			valid = valid && len(inputs) == 9 && counts["minecraft:paper"] == 8 && mapCentredInPaper(craft.input)
		case mapExtendingCartographyMultiRecipe:
			valid = valid && len(inputs) == 2 && counts["minecraft:paper"] == 1
		case mapCloningMultiRecipe:
			valid = valid && counts["minecraft:empty_map"] >= 1 && len(inputs) == counts["minecraft:empty_map"]+1
			outputCount *= counts["minecraft:empty_map"] + 1
		case mapCloningCartographyMultiRecipe:
			valid = valid && len(inputs) == 2 && counts["minecraft:empty_map"] == 1
			outputCount *= 2
		case mapLockingMultiRecipe:
			valid = valid && len(inputs) == 2 && counts["minecraft:glass_pane"] == 1
		}
		if !valid {
			return item.Stack{}, fmt.Errorf("map multi recipe contains invalid inputs")
		}
		if craft.times != 1 {
			return item.Stack{}, fmt.Errorf("map multi recipes require exactly one craft")
		}
		if id == mapCloningMultiRecipe || id == mapCloningCartographyMultiRecipe {
			return withCount(source, outputCount)
		}
		return withCount(source, 1)
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
		return withCount(source.WithItem(book), (writable+1)*craft.times)
	case bannerDuplicateMultiRecipe:
		if len(inputs) != 2 {
			return item.Stack{}, fmt.Errorf("banner duplication requires exactly two banners")
		}
		first, firstOK := inputs[0].Item().(block.Banner)
		second, secondOK := inputs[1].Item().(block.Banner)
		if !firstOK || !secondOK || first.Illager || second.Illager || first.Colour != second.Colour ||
			(len(first.Patterns) == 0) == (len(second.Patterns) == 0) || max(len(first.Patterns), len(second.Patterns)) > 6 || !validBannerDuplicateGrid(craft.input) {
			return item.Stack{}, fmt.Errorf("banner duplication requires one patterned and one blank matching banner")
		}
		source := inputs[0]
		if len(second.Patterns) != 0 {
			source = inputs[1]
		}
		return withCount(source, 2*craft.times)
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
		if !ok || sourceBanner.Illager || len(sourceBanner.Patterns) >= 6 || dyes == 0 || patterns > 1 {
			return item.Stack{}, fmt.Errorf("banner pattern recipe supplied invalid inputs")
		}

		var added block.BannerPatternType
		if patterns == 1 {
			if len(inputs) != 3 {
				return item.Stack{}, fmt.Errorf("banner template recipe requires one banner, dye and template")
			}
			added, ok = bannerPatternForItem(pattern.Type)
		} else {
			added, ok = bannerPatternFromGrid(craft.input)
		}
		if !ok {
			return item.Stack{}, fmt.Errorf("banner pattern recipe has no valid pattern for its inputs")
		}
		sourceBanner.Patterns = append(slices.Clone(sourceBanner.Patterns), block.BannerPatternLayer{Type: added, Colour: colour})
		return withCount(source.WithItem(sourceBanner), craft.times)
	default:
		return item.Stack{}, fmt.Errorf("unsupported multi recipe UUID %v", craft.recipe.UUID())
	}
}

// repairedMultiResult returns the item two damaged items of the same kind
// repair into: a new item of their durability and a twentieth more, with
// the curses of either at their highest level.
func repairedMultiResult(input []item.Stack) (item.Stack, error) {
	if len(input) != 2 {
		return item.Stack{}, fmt.Errorf("repair multi recipe requires exactly two matching durable items")
	}
	durability := 0
	for i, stack := range input {
		if stack.Empty() || stack.MaxDurability() < 1 || stack.Count() != 1 {
			return item.Stack{}, fmt.Errorf("repair multi recipe requires exactly two matching durable items")
		}
		name, meta := stack.Item().EncodeItem()
		if i != 0 {
			firstName, firstMeta := input[0].Item().EncodeItem()
			if name != firstName || meta != firstMeta {
				return item.Stack{}, fmt.Errorf("repair multi recipe requires exactly two matching durable items")
			}
		}
		durability += stack.Durability()
	}

	name, meta := input[0].Item().EncodeItem()
	base, ok := world.ItemByName(name, meta)
	if !ok {
		return item.Stack{}, fmt.Errorf("repair multi recipe item %q is not registered", name)
	}
	output := item.NewStack(base, 1)
	curses := map[item.EnchantmentType]item.Enchantment{}
	for _, stack := range input {
		for _, enchantment := range stack.Enchantments() {
			curse, ok := enchantment.Type().(interface{ Curse() bool })
			if !ok || !curse.Curse() {
				continue
			}
			if current, ok := curses[enchantment.Type()]; !ok || enchantment.Level() > current.Level() {
				curses[enchantment.Type()] = enchantment
			}
		}
	}
	output = output.WithForcedEnchantments(slices.Collect(maps.Values(curses))...)
	return output.WithDurability(min(output.MaxDurability(), durability+output.MaxDurability()/20)), nil
}

// mapCentredInPaper reports whether a 3x3 grid holds a filled map in the
// middle and paper around it.
func mapCentredInPaper(grid []item.Stack) bool {
	if len(grid) != 9 {
		return false
	}
	for slot, stack := range grid {
		if stack.Empty() {
			return false
		}
		name, _ := stack.Item().EncodeItem()
		if slot == 4 {
			if name != "minecraft:filled_map" {
				return false
			}
		} else if name != "minecraft:paper" {
			return false
		}
	}
	return true
}

// changedMapResult scales or locks the map of source, as operation says,
// and returns the filled map of the map that results.
func changedMapResult(source item.Stack, operation string, tx *world.Tx) (item.Stack, error) {
	mapItem, ok := source.Item().(item.FilledMap)
	if !ok {
		return item.Stack{}, fmt.Errorf("map transition requires one filled map")
	}
	if tx == nil {
		return item.Stack{}, world.ErrMapDataUnavailable
	}
	mapID := mapItem.NativeID
	if !mapItem.HasNativeID {
		if !mapItem.HasJavaID {
			return item.Stack{}, fmt.Errorf("filled map has no valid map ID")
		}
		var err error
		mapID, err = tx.World().MapIDByJavaID(mapItem.JavaID)
		if err != nil {
			return item.Stack{}, err
		}
	}

	var data world.MapData
	var err error
	switch operation {
	case mapExtendingMultiRecipe, mapExtendingCartographyMultiRecipe:
		data, err = tx.Maps().Scale(mapID)
	case mapLockingMultiRecipe:
		data, err = tx.Maps().Lock(mapID)
	default:
		return item.Stack{}, fmt.Errorf("unsupported map operation %v", operation)
	}
	if err != nil {
		return item.Stack{}, err
	}
	result := item.FilledMap{
		NativeID: data.ID, HasNativeID: true,
		JavaID: data.JavaID, HasJavaID: data.HasJavaID,
	}
	return source.WithItem(result).Grow(1 - source.Count()), nil
}

// validBannerDuplicateGrid reports whether a grid holds a patterned banner
// and a blank one, side by side or the blank one below.
func validBannerDuplicateGrid(grid []item.Stack) bool {
	width := 0
	switch len(grid) {
	case 4:
		width = 2
	case 9:
		width = 3
	default:
		return false
	}
	patterned, blank := -1, -1
	for slot, stack := range grid {
		banner, ok := stack.Item().(block.Banner)
		if !ok {
			continue
		}
		if len(banner.Patterns) == 0 {
			blank = slot
		} else {
			patterned = slot
		}
	}
	return patterned >= 0 && blank >= 0 &&
		(patterned+1 == blank && patterned/width == blank/width || patterned+width == blank)
}

// bannerPatternForItem returns the banner pattern a banner pattern item
// adds.
func bannerPatternForItem(pattern item.BannerPatternType) (block.BannerPatternType, bool) {
	for _, candidate := range block.BannerPatternTypes() {
		if candidatePattern, ok := candidate.Item(); ok && candidatePattern == pattern {
			return candidate, true
		}
	}
	return block.BannerPatternType{}, false
}

// bannerPatternFromGrid returns the banner pattern the dyes of a 3x3 grid
// around a banner draw.
func bannerPatternFromGrid(grid []item.Stack) (block.BannerPatternType, bool) {
	if len(grid) != 9 {
		return block.BannerPatternType{}, false
	}
	layout := make([]byte, 9)
	for slot, stack := range grid {
		switch stack.Item().(type) {
		case nil:
			layout[slot] = '.'
		case block.Banner:
			layout[slot] = 'B'
		case item.Dye:
			layout[slot] = 'D'
		default:
			return block.BannerPatternType{}, false
		}
	}
	switch string(layout) {
	case "....B.DDD":
		return block.StripeBottomBannerPattern(), true
	case "DDD....B.":
		return block.StripeTopBannerPattern(), true
	case "D..D..DB.":
		return block.StripeLeftBannerPattern(), true
	case "..D..D.BD":
		return block.StripeRightBannerPattern(), true
	case ".D..DB.D.":
		return block.StripeCentreBannerPattern(), true
	case "...DDD.B.":
		return block.StripeMiddleBannerPattern(), true
	case "D...D..BD":
		return block.StripeDownRightBannerPattern(), true
	case "..D.D.DB.":
		return block.StripeDownLeftBannerPattern(), true
	case "D.DD.D.B.":
		return block.SmallStripesBannerPattern(), true
	case "D.D.D.DBD":
		return block.CrossBannerPattern(), true
	case ".D.DDDBD.":
		return block.StraightCrossBannerPattern(), true
	case "DD.D...B.":
		return block.DiagonalLeftBannerPattern(), true
	case ".DD..D.B.":
		return block.DiagonalRightBannerPattern(), true
	case ".B.D..DD.":
		return block.DiagonalUpLeftBannerPattern(), true
	case ".B...D.DD":
		return block.DiagonalUpRightBannerPattern(), true
	case "DD.DDBDD.":
		return block.HalfVerticalBannerPattern(), true
	case ".DDBDD.DD":
		return block.HalfVerticalRightBannerPattern(), true
	case "DDDDDD.B.":
		return block.HalfHorizontalBannerPattern(), true
	case ".B.DDDDDD":
		return block.HalfHorizontalBottomBannerPattern(), true
	case "......DB.":
		return block.SquareBottomLeftBannerPattern(), true
	case ".......BD":
		return block.SquareBottomRightBannerPattern(), true
	case "D......B.":
		return block.SquareTopLeftBannerPattern(), true
	case "..D....B.":
		return block.SquareTopRightBannerPattern(), true
	case "....D.DBD":
		return block.TriangleBottomBannerPattern(), true
	case "D.D.D..B.":
		return block.TriangleTopBannerPattern(), true
	case "...DBD.D.":
		return block.TrianglesBottomBannerPattern(), true
	case ".D.D.D.B.":
		return block.TrianglesTopBannerPattern(), true
	case "....D..B.":
		return block.CircleBannerPattern(), true
	case ".D.DBD.D.":
		return block.RhombusBannerPattern(), true
	case "DDDDBDDDD":
		return block.BorderBannerPattern(), true
	case "DBD.D..D.":
		return block.GradientBannerPattern(), true
	case ".D..D.DBD":
		return block.GradientUpBannerPattern(), true
	}
	return block.BannerPatternType{}, false
}
