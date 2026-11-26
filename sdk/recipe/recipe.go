// Package recipe provides clean constructors and registration helpers for
// shaped, shapeless, smithing, brewing, and dynamic crafting recipes.
package recipe

import (
	"fmt"

	"github.com/dm-vev/adamant/server/item"
	core "github.com/dm-vev/adamant/server/item/recipe"
	"github.com/dm-vev/adamant/server/world"
)

// Type aliases for core recipe interfaces.
type (
	Recipe        = core.Recipe
	DynamicRecipe = core.DynamicRecipe
	Ingredient    = core.Item
)

// Option configures optional recipe metadata such as crafting station block.
type Option func(*opts)

type opts struct {
	station          string
	preserveUserData bool
}

// Station overrides the crafting station block name (e.g. "crafting_table",
// "smithing_table", "stonecutter").
func Station(block string) Option { return func(o *opts) { o.station = block } }

// PreserveUserData marks a shapeless recipe as UserDataShapeless.
func PreserveUserData() Option { return func(o *opts) { o.preserveUserData = true } }

// Shaped builds a shaped recipe from ASCII pattern rows (1..3 rows, 1..3 cols)
// and a rune->ingredient map. Spaces (' ') represent empty slots.
func Shaped(output item.Stack, pattern []string, key map[rune]Ingredient, options ...Option) core.Shaped {
	if len(pattern) == 0 || len(pattern) > 3 {
		panic(fmt.Sprintf("sdk/recipe: pattern must have 1..3 rows, got %d", len(pattern)))
	}
	width := len([]rune(pattern[0]))
	if width == 0 || width > 3 {
		panic(fmt.Sprintf("sdk/recipe: pattern width must be 1..3, got %d", width))
	}
	inputs := make([]Ingredient, 0, width*len(pattern))
	for rowIdx, row := range pattern {
		runes := []rune(row)
		if len(runes) != width {
			panic(fmt.Sprintf("sdk/recipe: pattern row %d width %d != %d", rowIdx, len(runes), width))
		}
		for _, ch := range runes {
			if ch == ' ' {
				inputs = append(inputs, item.Stack{})
				continue
			}
			ing, ok := key[ch]
			if !ok {
				panic(fmt.Sprintf("sdk/recipe: missing key mapping for %q", ch))
			}
			inputs = append(inputs, ing)
		}
	}
	o := applyOpts("crafting_table", options)
	return core.NewShaped(inputs, output, core.NewShape(width, len(pattern)), o.station)
}

// Shapeless builds an order-independent crafting recipe.
func Shapeless(output item.Stack, inputs []Ingredient, options ...Option) Recipe {
	if len(inputs) == 0 {
		panic("sdk/recipe: shapeless inputs must not be empty")
	}
	o := applyOpts("crafting_table", options)
	if o.preserveUserData {
		return core.NewUserDataShapeless(inputs, output, o.station)
	}
	return core.NewShapeless(inputs, output, o.station)
}

// Smithing builds a smithing table transform recipe.
func Smithing(output item.Stack, template, base, addition Ingredient, options ...Option) core.SmithingTransform {
	o := applyOpts("smithing_table", options)
	return core.NewSmithingTransform(base, addition, template, output, o.station)
}

// Brewing builds a brewing stand potion mixing recipe.
func Brewing(output item.Stack, input, reagent Ingredient) core.Potion {
	return core.NewPotion(input, reagent, output)
}

// BrewingContainer builds a brewing stand container conversion recipe
// (e.g. drinkable -> splash/lingering).
func BrewingContainer(input, output world.Item, reagent item.Stack) core.PotionContainerChange {
	return core.NewPotionContainerChange(input, output, reagent)
}

type funcDynamic struct {
	station string
	match   func([]Ingredient) ([]item.Stack, bool)
}

func (f funcDynamic) Block() string { return f.station }
func (f funcDynamic) Match(in []Ingredient) ([]item.Stack, bool) {
	if f.match == nil {
		return nil, false
	}
	return f.match(in)
}

// Dynamic builds a server-side dynamic crafting recipe evaluated at craft time.
func Dynamic(match func(input []Ingredient) (output []item.Stack, ok bool), options ...Option) DynamicRecipe {
	o := applyOpts("crafting_table", options)
	return funcDynamic{station: o.station, match: match}
}

// Register registers one or more recipes in the global recipe registry.
func Register(recipes ...Recipe) {
	for _, r := range recipes {
		if r != nil {
			core.Register(r)
		}
	}
}

// RegisterDynamic registers one or more server-side dynamic recipes.
func RegisterDynamic(recipes ...DynamicRecipe) {
	for _, r := range recipes {
		if r != nil {
			core.RegisterDynamic(r)
		}
	}
}

func applyOpts(defaultStation string, options []Option) opts {
	o := opts{station: defaultStation}
	for _, opt := range options {
		if opt != nil {
			opt(&o)
		}
	}
	return o
}
