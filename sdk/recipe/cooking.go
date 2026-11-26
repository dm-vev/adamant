package recipe

import (
	"github.com/dm-vev/adamant/server/item"
	core "github.com/dm-vev/adamant/server/item/recipe"
	"github.com/dm-vev/adamant/server/world"
)

// CookingKind selects which cooking station processes a CookingEntry.
type CookingKind = item.CookingKind

const (
	KindSmelting        = item.Smelting
	KindBlasting        = item.Blasting
	KindSmoking         = item.Smoking
	KindCampfireCooking = item.CampfireCooking
)

// CookingOption configures optional cooking recipe fields.
type CookingOption func(*item.CookingRecipe)

// CookingID sets the recipe identifier string.
func CookingID(id string) CookingOption { return func(r *item.CookingRecipe) { r.ID = id } }

// CookingXP sets the experience granted per smelted item.
func CookingXP(xp float32) CookingOption { return func(r *item.CookingRecipe) { r.Experience = xp } }

// CookingTime sets the cook duration in ticks (defaults to 200 for furnace,
// 100 for blast furnace/smoker, 600 for campfire).
func CookingTime(ticks int) CookingOption {
	return func(r *item.CookingRecipe) { r.CookingTime = ticks }
}

// CookingEntry pairs a CookingKind and input item with its CookingRecipe.
type CookingEntry struct {
	Kind   CookingKind
	Input  world.Item
	Recipe item.CookingRecipe
}

func makeCooking(kind CookingKind, defaultTicks int, input world.Item, output item.Stack, opts ...CookingOption) CookingEntry {
	r := item.CookingRecipe{
		Product:     output,
		CookingTime: defaultTicks,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&r)
		}
	}
	return CookingEntry{Kind: kind, Input: input, Recipe: r}
}

// Smelting builds a furnace smelting recipe.
func Smelting(input world.Item, output item.Stack, opts ...CookingOption) CookingEntry {
	return makeCooking(KindSmelting, 200, input, output, opts...)
}

// Blasting builds a blast furnace recipe.
func Blasting(input world.Item, output item.Stack, opts ...CookingOption) CookingEntry {
	return makeCooking(KindBlasting, 100, input, output, opts...)
}

// Smoking builds a smoker recipe.
func Smoking(input world.Item, output item.Stack, opts ...CookingOption) CookingEntry {
	return makeCooking(KindSmoking, 100, input, output, opts...)
}

// Campfire builds a campfire cooking recipe.
func Campfire(input world.Item, output item.Stack, opts ...CookingOption) CookingEntry {
	return makeCooking(KindCampfireCooking, 600, input, output, opts...)
}

// RegisterCooking registers one or more furnace, blast furnace, smoker, or
// campfire recipes.
func RegisterCooking(entries ...CookingEntry) {
	for _, e := range entries {
		if e.Input != nil {
			item.RegisterCookingRecipe(e.Kind, e.Input, e.Recipe)
		}
	}
}

// RegisterFuel registers or overrides the furnace burn duration in ticks for it.
func RegisterFuel(it world.Item, burnTicks int) {
	if it != nil {
		item.RegisterCookingFuel(it, burnTicks)
	}
}

// Stonecutter builds a stonecutter recipe converting input into output.
func Stonecutter(input Ingredient, output item.Stack) core.Shapeless {
	return core.NewShapeless([]Ingredient{input}, output, "stonecutter")
}
