package recipe_test

import (
	"testing"

	"github.com/df-mc/dragonfly/sdk/recipe"
	"github.com/df-mc/dragonfly/server/item"
)

func TestRecipeBuildersAndRegistration(t *testing.T) {
	out := item.NewStack(item.Diamond{}, 1)
	shaped := recipe.Shaped(out, []string{
		" C ",
		"CCC",
		" C ",
	}, map[rune]recipe.Ingredient{
		'C': item.NewStack(item.Coal{}, 1),
	})
	if shaped.Block() != "crafting_table" {
		t.Fatalf("shaped station = %q", shaped.Block())
	}

	shapeless := recipe.Shapeless(
		item.NewStack(item.Emerald{}, 1),
		[]recipe.Ingredient{item.NewStack(item.Coal{}, 1), item.NewStack(item.IronIngot{}, 1)},
		recipe.PreserveUserData(),
	)
	smithing := recipe.Smithing(
		item.NewStack(item.NetheriteIngot{}, 1),
		item.NewStack(item.Paper{}, 1),
		item.NewStack(item.GoldIngot{}, 1),
		item.NewStack(item.Diamond{}, 1),
	)
	brewing := recipe.Brewing(
		item.NewStack(item.GlowstoneDust{}, 1),
		item.NewStack(item.GlassBottle{}, 1),
		item.NewStack(item.Sugar{}, 1),
	)
	recipe.Register(shaped, shapeless, smithing, brewing)

	dyn := recipe.Dynamic(func(in []recipe.Ingredient) ([]item.Stack, bool) {
		if len(in) == 1 {
			return []item.Stack{item.NewStack(item.Stick{}, 4)}, true
		}
		return nil, false
	})
	if res, ok := dyn.Match([]recipe.Ingredient{item.NewStack(item.Coal{}, 1)}); !ok || len(res) != 1 || res[0].Count() != 4 {
		t.Fatalf("dynamic match = (%v, %v)", res, ok)
	}
	recipe.RegisterDynamic(dyn)

	// Cooking (Smelting/Blasting/Smoking/Campfire), custom fuel, and Stonecutter.
	recipe.RegisterCooking(
		recipe.Smelting(item.Paper{}, item.NewStack(item.Coal{}, 2), recipe.CookingID("sdk:paper_to_coal"), recipe.CookingXP(0.5), recipe.CookingTime(120)),
		recipe.Blasting(item.GlassBottle{}, item.NewStack(item.Diamond{}, 1)),
		recipe.Smoking(item.Sugar{}, item.NewStack(item.Cookie{}, 2)),
		recipe.Campfire(item.Stick{}, item.NewStack(item.Charcoal{}, 1)),
	)
	gotCook, ok := item.CookingRecipeFor(item.Smelting, item.Paper{})
	if !ok || gotCook.Product.Count() != 2 || gotCook.CookingTime != 120 {
		t.Fatalf("CookingRecipeFor(Smelting, Paper) = (%+v, %v)", gotCook, ok)
	}
	recipe.RegisterFuel(item.Paper{}, 400)
	if burn := item.CookingBurnTime(item.Paper{}, false); burn != 400 {
		t.Fatalf("CookingBurnTime(Paper) = %d, want 400", burn)
	}
	sc := recipe.Stonecutter(item.NewStack(item.Coal{}, 1), item.NewStack(item.Stick{}, 2))
	if sc.Block() != "stonecutter" {
		t.Fatalf("Stonecutter block = %q", sc.Block())
	}
	recipe.Register(sc)
}
