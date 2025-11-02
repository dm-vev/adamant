package recipe

import "github.com/df-mc/dragonfly/server/item"

func init() {
	inputs := []Item{
		item.NewStack(item.Stick{}, 1), item.NewStack(item.Stick{}, 1), item.NewStack(item.Stick{}, 1),
		item.NewStack(item.Stick{}, 1), NewItemTag("minecraft:wool", 1), item.NewStack(item.Stick{}, 1),
		item.NewStack(item.Stick{}, 1), item.NewStack(item.Stick{}, 1), item.NewStack(item.Stick{}, 1),
	}
	Register(NewShaped(inputs, item.NewStack(item.Painting{}, 1), NewShape(3, 3), "crafting_table"))
}
