package recipe

import "github.com/df-mc/dragonfly/server/item"

func init() {
	Register(NewShaped([]Item{
		item.NewStack(item.String{}, 1),
		item.NewStack(item.Leather{}, 1),
	}, item.NewStack(item.Bundle{}, 1), NewShape(1, 2), "crafting_table"))

	bundles := []item.Bundle{{}}
	for _, colour := range item.Colours() {
		bundles = append(bundles, item.Bundle{Colour: colour, Dyed: true})
	}
	for _, bundle := range bundles {
		for _, colour := range item.Colours() {
			Register(NewUserDataShapeless([]Item{
				item.NewStack(bundle, 1),
				item.NewStack(item.Dye{Colour: colour}, 1),
			}, item.NewStack(item.Bundle{Colour: colour, Dyed: true}, 1), "crafting_table"))
		}
	}
}
