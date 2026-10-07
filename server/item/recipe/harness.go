package recipe

import (
	"github.com/df-mc/dragonfly/server/item"
)


func init() {
	glass := item.NewStack(encodedBlockItem("minecraft:glass"), 1)
	leather := item.NewStack(item.Leather{}, 1)
	for _, colour := range item.Colours() {
		harness := item.Harness{Colour: colour}
		Register(NewShaped([]Item{
			leather, leather, leather,
			glass, item.NewStack(encodedBlockItem("minecraft:"+colour.String()+"_wool"), 1), glass,
		}, item.NewStack(harness, 1), NewShape(3, 2), "crafting_table"))
		for _, sourceColour := range item.Colours() {
			if sourceColour == colour {
				continue
			}
			Register(NewShapeless([]Item{
				item.NewStack(item.Dye{Colour: colour}, 1),
				item.NewStack(item.Harness{Colour: sourceColour}, 1),
			}, item.NewStack(harness, 1), "crafting_table"))
		}
	}
}
