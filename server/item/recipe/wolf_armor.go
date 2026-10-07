package recipe

import (
	"image/color"

	"github.com/df-mc/dragonfly/server/item"
)

type wolfArmorDyeRecipe struct{}

func (wolfArmorDyeRecipe) Block() string { return "crafting_table" }

func (wolfArmorDyeRecipe) Match(input []Item) ([]item.Stack, bool) {
	var armourStack item.Stack
	var armour item.WolfArmor
	var hasArmour bool
	dyes := make([]item.Colour, 0, len(input))
	for _, ingredient := range input {
		stack, ok := ingredient.(item.Stack)
		if !ok || stack.Empty() {
			continue
		}
		switch typed := stack.Item().(type) {
		case item.WolfArmor:
			armourStack = stack
			if hasArmour {
				return nil, false
			}
			armour, hasArmour = typed, true
		case item.Dye:
			dyes = append(dyes, typed.Colour)
		default:
			return nil, false
		}
	}
	if !hasArmour || len(dyes) == 0 {
		return nil, false
	}
	armour.Colour = mixWolfArmorDyes(armour.Colour, dyes)
	return []item.Stack{armourStack.WithItem(armour)}, true
}

func mixWolfArmorDyes(current color.RGBA, dyes []item.Colour) color.RGBA {
	redTotal, greenTotal, blueTotal, intensityTotal, count := 0, 0, 0, 0, 0
	add := func(colour color.RGBA) {
		redTotal += int(colour.R)
		greenTotal += int(colour.G)
		blueTotal += int(colour.B)
		intensityTotal += max(int(colour.R), int(colour.G), int(colour.B))
		count++
	}
	if current != (color.RGBA{}) {
		add(current)
	}
	for _, dye := range dyes {
		add(dye.RGBA())
	}
	red, green, blue := redTotal/count, greenTotal/count, blueTotal/count
	averageIntensity := float32(intensityTotal) / float32(count)
	resultIntensity := max(red, green, blue)
	return color.RGBA{
		R: uint8(float32(red) * averageIntensity / float32(resultIntensity)),
		G: uint8(float32(green) * averageIntensity / float32(resultIntensity)),
		B: uint8(float32(blue) * averageIntensity / float32(resultIntensity)),
		A: 255,
	}
}

func init() {
	empty := NewItemTag("", 0)
	scute := item.NewStack(item.ArmadilloScute{}, 1)
	Register(NewShaped([]Item{
		scute, empty, empty,
		scute, scute, scute,
		scute, empty, scute,
	}, item.NewStack(item.WolfArmor{}, 1), NewShape(3, 3), "crafting_table"))
	RegisterDynamic(wolfArmorDyeRecipe{})
}
