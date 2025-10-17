package recipe

import "github.com/df-mc/dragonfly/server/item"

func init() {
	Register(NewShapeless([]Item{
		item.NewStack(item.BreezeRod{}, 1),
		item.NewStack(item.HeavyCore{}, 1),
	}, item.NewStack(item.Mace{}, 1), "crafting_table"))
}
