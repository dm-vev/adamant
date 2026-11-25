package api

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/world"
)

// ShapedRecipeSpec declaratively defines a shaped crafting grid recipe using
// ASCII pattern rows (1-3 rows, 1-3 columns) and a symbol->ingredient map.
// Spaces (' ') denote empty grid slots.
type ShapedRecipeSpec struct {
	Pattern []string
	Key     map[rune]recipe.Item
	Output  item.Stack
	// Block specifies the crafting station (defaults to "crafting_table").
	Block string
}

// RegisterShapedRecipe compiles spec into a recipe.Shaped and registers it.
func RegisterShapedRecipe(spec ShapedRecipeSpec) recipe.Shaped {
	if len(spec.Pattern) == 0 || len(spec.Pattern) > 3 {
		panic(fmt.Sprintf("api: ShapedRecipeSpec.Pattern must have 1..3 rows, got %d", len(spec.Pattern)))
	}
	width := len([]rune(spec.Pattern[0]))
	if width == 0 || width > 3 {
		panic(fmt.Sprintf("api: ShapedRecipeSpec.Pattern row width must be 1..3, got %d", width))
	}
	inputs := make([]recipe.Item, 0, width*len(spec.Pattern))
	for rIdx, row := range spec.Pattern {
		runes := []rune(row)
		if len(runes) != width {
			panic(fmt.Sprintf("api: ShapedRecipeSpec.Pattern row %d has width %d, want %d", rIdx, len(runes), width))
		}
		for _, ch := range runes {
			if ch == ' ' {
				inputs = append(inputs, item.Stack{})
				continue
			}
			ing, ok := spec.Key[ch]
			if !ok {
				panic(fmt.Sprintf("api: ShapedRecipeSpec missing Key mapping for %q", ch))
			}
			inputs = append(inputs, ing)
		}
	}
	station := spec.Block
	if station == "" {
		station = "crafting_table"
	}
	r := recipe.NewShaped(inputs, spec.Output, recipe.NewShape(width, len(spec.Pattern)), station)
	recipe.Register(r)
	return r
}

// ShapelessRecipeSpec declaratively defines an order-independent crafting recipe.
type ShapelessRecipeSpec struct {
	Inputs []recipe.Item
	Output item.Stack
	// Block specifies the crafting station (defaults to "crafting_table").
	Block string
	// PreserveUserData registers a UserDataShapeless recipe when true.
	PreserveUserData bool
}

// RegisterShapelessRecipe registers a shapeless crafting recipe from spec.
func RegisterShapelessRecipe(spec ShapelessRecipeSpec) recipe.Recipe {
	if len(spec.Inputs) == 0 {
		panic("api: ShapelessRecipeSpec.Inputs must not be empty")
	}
	station := spec.Block
	if station == "" {
		station = "crafting_table"
	}
	if spec.PreserveUserData {
		r := recipe.NewUserDataShapeless(spec.Inputs, spec.Output, station)
		recipe.Register(r)
		return r
	}
	r := recipe.NewShapeless(spec.Inputs, spec.Output, station)
	recipe.Register(r)
	return r
}

// SmithingRecipeSpec declaratively defines a smithing table transform recipe.
type SmithingRecipeSpec struct {
	Template recipe.Item
	Base     recipe.Item
	Addition recipe.Item
	Output   item.Stack
	Block    string
}

// RegisterSmithingRecipe registers a smithing transform recipe from spec.
func RegisterSmithingRecipe(spec SmithingRecipeSpec) recipe.SmithingTransform {
	station := spec.Block
	if station == "" {
		station = "smithing_table"
	}
	r := recipe.NewSmithingTransform(spec.Base, spec.Addition, spec.Template, spec.Output, station)
	recipe.Register(r)
	return r
}

// PotionRecipeSpec declaratively defines a brewing stand potion recipe.
type PotionRecipeSpec struct {
	Input   recipe.Item
	Reagent recipe.Item
	Output  item.Stack
}

// RegisterPotionRecipe registers a brewing stand potion recipe from spec.
func RegisterPotionRecipe(spec PotionRecipeSpec) recipe.Potion {
	r := recipe.NewPotion(spec.Input, spec.Reagent, spec.Output)
	recipe.Register(r)
	return r
}

// RegisterPotionContainerChangeRecipe registers a brewing container conversion
// recipe (e.g., drinkable -> splash or lingering).
func RegisterPotionContainerChangeRecipe(input, output world.Item, reagent item.Stack) recipe.PotionContainerChange {
	r := recipe.NewPotionContainerChange(input, output, reagent)
	recipe.Register(r)
	return r
}

// DynamicRecipeSpec declaratively defines a server-side dynamic crafting recipe.
type DynamicRecipeSpec struct {
	Block string
	Match func(input []recipe.Item) (output []item.Stack, ok bool)
}

type funcDynamicRecipe struct {
	block string
	match func(input []recipe.Item) ([]item.Stack, bool)
}

func (f funcDynamicRecipe) Block() string { return f.block }
func (f funcDynamicRecipe) Match(input []recipe.Item) ([]item.Stack, bool) {
	if f.match == nil {
		return nil, false
	}
	return f.match(input)
}

// RegisterDynamicRecipe registers a server-side dynamic recipe validated at craft time.
func RegisterDynamicRecipe(spec DynamicRecipeSpec) recipe.DynamicRecipe {
	station := spec.Block
	if station == "" {
		station = "crafting_table"
	}
	r := funcDynamicRecipe{block: station, match: spec.Match}
	recipe.RegisterDynamic(r)
	return r
}

// RegisterRecipe registers any recipe.Recipe directly.
func RegisterRecipe(r recipe.Recipe) {
	if r != nil {
		recipe.Register(r)
	}
}
