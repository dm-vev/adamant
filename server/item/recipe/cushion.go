package recipe

import "github.com/df-mc/dragonfly/server/item"

func init() {
	for _, colour := range item.Colours() {
		cushion := item.Cushion{Colour: colour}
		Register(NewShaped([]Item{
			item.NewStack(encodedBlockItem("minecraft:"+colour.String()+"_wool_slab"), 1),
			item.NewStack(encodedBlockItem("minecraft:"+colour.String()+"_wool_slab"), 1),
			item.NewStack(encodedBlockItem("minecraft:"+colour.String()+"_wool_slab"), 1),
		}, item.NewStack(cushion, 1), NewShape(3, 1), "crafting_table"))
		for _, sourceColour := range item.Colours() {
			if sourceColour == colour {
				continue
			}
			Register(NewShapeless([]Item{
				item.NewStack(item.Dye{Colour: colour}, 1),
				item.NewStack(item.Cushion{Colour: sourceColour}, 1),
			}, item.NewStack(cushion, 1), "crafting_table"))
		}
	}
}
