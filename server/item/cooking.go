package item

import (
	"fmt"
	"sync"

	"github.com/df-mc/dragonfly/server/world"
)

// CookingKind is a kind of Java cooking recipe: the recipes furnaces, blast
// furnaces, smokers and campfires cook.
type CookingKind uint8

const (
	// Smelting recipes are cooked by furnaces.
	Smelting CookingKind = iota
	// Blasting recipes are cooked by blast furnaces.
	Blasting
	// Smoking recipes are cooked by smokers.
	Smoking
	// CampfireCooking recipes are cooked by campfires.
	CampfireCooking
)

// CookingRecipe is a Java cooking recipe, an AbstractCookingRecipe.
type CookingRecipe struct {
	// ID is the identifier of the recipe, such as
	// minecraft:iron_ingot_from_smelting_raw_iron.
	ID string
	// Product is the stack the recipe produces.
	Product Stack
	// Experience is the experience a cooked item is worth.
	Experience float32
	// CookingTime is the time in ticks the recipe takes before a furnace's
	// speed multiplier is applied.
	CookingTime int
}

// javaCookingRecipe is a generated Java cooking recipe naming its items by
// Java identifier.
type javaCookingRecipe struct {
	kind           CookingKind
	ingredient, id string
	result         string
	count          int
	experience     float32
	cookingTime    int
}

var (
	cookingOnce    sync.Once
	cookingByInput map[cookingKey]CookingRecipe
	cookingByID    map[string]CookingRecipe
	cookingFuel    map[cookingItemKey]int
)

type cookingItemKey struct {
	name string
	meta int16
}

type cookingKey struct {
	kind CookingKind
	item cookingItemKey
}

func cookingItem(it world.Item) cookingItemKey {
	name, meta := it.EncodeItem()
	return cookingItemKey{name: name, meta: meta}
}

// loadCooking resolves the generated recipes and fuels to registered items.
// It runs on first use, once every item is registered.
func loadCooking() {
	cookingByInput = make(map[cookingKey]CookingRecipe, len(javaCookingRecipes))
	cookingByID = make(map[string]CookingRecipe, len(javaCookingRecipes))
	for _, r := range javaCookingRecipes {
		in, ok := javaItem(r.ingredient)
		if !ok {
			continue
		}
		out, ok := javaItem(r.result)
		if !ok {
			panic(fmt.Sprintf("cooking recipe %v produces unknown item %v", r.id, r.result))
		}
		recipe := CookingRecipe{ID: r.id, Product: NewStack(out, r.count), Experience: r.experience, CookingTime: r.cookingTime}
		cookingByInput[cookingKey{kind: r.kind, item: cookingItem(in)}] = recipe
		cookingByID[r.id] = recipe
	}
	cookingFuel = make(map[cookingItemKey]int, len(javaCookingFuel))
	for id, ticks := range javaCookingFuel {
		if it, ok := javaItem(id); ok {
			cookingFuel[cookingItem(it)] = ticks
		}
	}
}

// CookingRecipeFor returns the recipe of the kind passed that has the item as
// its ingredient, as RecipeManager.getRecipeFor.
func CookingRecipeFor(kind CookingKind, it world.Item) (CookingRecipe, bool) {
	if it == nil {
		return CookingRecipe{}, false
	}
	cookingOnce.Do(loadCooking)
	r, ok := cookingByInput[cookingKey{kind: kind, item: cookingItem(it)}]
	return r, ok
}

// CookingRecipeByID returns the cooking recipe with the identifier passed.
func CookingRecipeByID(id string) (CookingRecipe, bool) {
	cookingOnce.Do(loadCooking)
	r, ok := cookingByID[id]
	return r, ok
}

// CookingBurnTime returns the ticks the item burns for as furnace fuel, or 0
// if it is no fuel, as its cooking_fuel component resolves: smokers and blast
// furnaces, the fast furnaces, burn fuel in half the time.
func CookingBurnTime(it world.Item, fast bool) int {
	if it == nil {
		return 0
	}
	cookingOnce.Do(loadCooking)
	ticks := cookingFuel[cookingItem(it)]
	if fast {
		return ticks / 2
	}
	return ticks
}

// CookingFuelRemainder returns what burning the item as fuel leaves behind,
// its crafting remainder: an empty bucket for a lava bucket.
func CookingFuelRemainder(it world.Item) (Stack, bool) {
	if b, ok := it.(Bucket); ok && b.Content.liquid != nil && b.Content.liquid.LiquidType() == "lava" {
		return NewStack(Bucket{}, 1), true
	}
	return Stack{}, false
}
