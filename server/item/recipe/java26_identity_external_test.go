package recipe_test

import (
	"reflect"
	"testing"

	_ "github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/world"
)

func TestJavaRecipeIDsMatchShapedAndSmithingTrimRecipes(t *testing.T) {
	planks, ok := world.ItemByName("minecraft:acacia_planks", 0)
	if !ok {
		t.Fatal("acacia planks item is unavailable")
	}
	pressurePlate, ok := world.ItemByName("minecraft:acacia_pressure_plate", 0)
	if !ok {
		t.Fatal("acacia pressure plate item is unavailable")
	}
	plank := item.NewStack(planks, 1)
	matching := recipe.NewShaped([]recipe.Item{plank, plank}, item.NewStack(pressurePlate, 1), recipe.NewShape(2, 1), "crafting_table")
	if got, want := recipe.JavaRecipeIDs(matching), []string{"minecraft:acacia_pressure_plate"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("matching shaped recipe IDs = %v, want %v", got, want)
	}

	wrongShape := recipe.NewShaped([]recipe.Item{plank, plank}, item.NewStack(pressurePlate, 1), recipe.NewShape(1, 2), "crafting_table")
	if got := recipe.JavaRecipeIDs(wrongShape); len(got) != 0 {
		t.Fatalf("wrong-shaped recipe IDs = %v, want none", got)
	}
	diamond := item.NewStack(item.Diamond{}, 1)
	wrongInput := recipe.NewShaped([]recipe.Item{diamond, diamond}, item.NewStack(pressurePlate, 1), recipe.NewShape(2, 1), "crafting_table")
	if got := recipe.JavaRecipeIDs(wrongInput); len(got) != 0 {
		t.Fatalf("wrong-input recipe IDs = %v, want none", got)
	}

	boltTemplate, ok := world.ItemByName("minecraft:bolt_armor_trim_smithing_template", 0)
	if !ok {
		t.Fatal("bolt smithing template item is unavailable")
	}
	base, addition := recipe.NewItemTag("minecraft:trimmable_armor", 1), recipe.NewItemTag("minecraft:trim_materials", 1)
	matchingTrim := recipe.NewSmithingTrim(base, addition, item.NewStack(boltTemplate, 1), "smithing_table")
	boltID := "minecraft:bolt_armor_trim_smithing_template_smithing_trim"
	if got, want := recipe.JavaRecipeIDs(matchingTrim), []string{boltID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("matching smithing trim IDs = %v, want %v", got, want)
	}

	coastTemplate, ok := world.ItemByName("minecraft:coast_armor_trim_smithing_template", 0)
	if !ok {
		t.Fatal("coast smithing template item is unavailable")
	}
	mismatchedTrim := recipe.NewSmithingTrim(base, addition, item.NewStack(coastTemplate, 1), "smithing_table")
	coastID := "minecraft:coast_armor_trim_smithing_template_smithing_trim"
	if got, want := recipe.JavaRecipeIDs(mismatchedTrim), []string{coastID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("mismatched smithing trim IDs = %v, want %v", got, want)
	}
}
