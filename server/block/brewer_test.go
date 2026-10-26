package block

import (
	"sync"
	"testing"
	_ "unsafe"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/world"
)

// registerVanillaRecipes is the vanilla recipe registration the server runs
// on start.
//
//go:linkname registerVanillaRecipes github.com/df-mc/dragonfly/server/item/recipe.registerVanilla
func registerVanillaRecipes()

var brewingRecipesOnce sync.Once

func brewingTestStand(t *testing.T) *brewer {
	t.Helper()
	brewingRecipesOnce.Do(func() {
		world.DefaultBlockRegistry.Finalize()
		registerVanillaRecipes()
	})
	return newBrewer()
}

// brewTicks ticks the brewer n times.
func brewTicks(w *world.World, b *brewer, n int) {
	<-w.Exec(func(tx *world.Tx) {
		for range n {
			b.tickBrewing("brewing_stand", cube.Pos{}, tx)
		}
	})
}

// TestBrewingFollowsJava follows BrewingStandBlockEntity.serverTick: blaze
// powder gives 20 brews, a brew takes one as it starts in the first tick and
// finishes 400 ticks after.
func TestBrewingFollowsJava(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	b := brewingTestStand(t)
	_ = b.inventory.SetItem(0, item.NewStack(NetherWart{}, 2))
	_ = b.inventory.SetItem(1, item.NewStack(item.Potion{Type: potion.Water()}, 1))
	_ = b.inventory.SetItem(4, item.NewStack(item.BlazePowder{}, 1))
	brewTicks(w, b, 1)
	if fuel, _ := b.inventory.Item(4); b.fuelAmount != 19 || b.fuelTotal != 20 || b.brewTime != 400 || !fuel.Empty() {
		t.Fatalf("after starting: fuel %v/%v, brew time %v, powder %v; want 19/20, 400, 0", b.fuelAmount, b.fuelTotal, b.brewTime, fuel.Count())
	}
	brewTicks(w, b, 399)
	if p, _ := b.inventory.Item(1); p.Item() != (item.Potion{Type: potion.Water()}) {
		t.Fatalf("brewed after 400 ticks")
	}
	brewTicks(w, b, 1)
	p, _ := b.inventory.Item(1)
	ingredient, _ := b.inventory.Item(0)
	if p.Item() != (item.Potion{Type: potion.Awkward()}) || ingredient.Count() != 1 || b.brewTime != 0 {
		t.Fatalf("after 401 ticks: potion %v, wart %v, brew time %v", p.Item(), ingredient.Count(), b.brewTime)
	}
}

// TestBrewingStopsWhenTheIngredientChanges cancels a brew whose ingredient
// is swapped, keeping the brew it took from the fuel spent.
func TestBrewingStopsWhenTheIngredientChanges(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	b := brewingTestStand(t)
	_ = b.inventory.SetItem(0, item.NewStack(item.Sugar{}, 1))
	_ = b.inventory.SetItem(1, item.NewStack(item.Potion{Type: potion.Awkward()}, 1))
	_ = b.inventory.SetItem(4, item.NewStack(item.BlazePowder{}, 1))
	brewTicks(w, b, 100)
	_ = b.inventory.SetItem(0, item.NewStack(item.RabbitFoot{}, 1))
	brewTicks(w, b, 1)
	if b.brewTime != 0 || b.fuelAmount != 19 {
		t.Fatalf("after swapping the ingredient: brew time %v, fuel %v; want 0, 19", b.brewTime, b.fuelAmount)
	}
	brewTicks(w, b, 1)
	if b.brewTime != 400 || b.fuelAmount != 18 {
		t.Fatalf("restarting with the new ingredient: brew time %v, fuel %v; want 400, 18", b.brewTime, b.fuelAmount)
	}
}

// TestDragonBreathLeavesABottle leaves the crafting remainder of dragon's
// breath, a glass bottle, in the ingredient slot.
func TestDragonBreathLeavesABottle(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	b := brewingTestStand(t)
	_ = b.inventory.SetItem(0, item.NewStack(item.DragonBreath{}, 1))
	_ = b.inventory.SetItem(2, item.NewStack(item.SplashPotion{Type: potion.Healing()}, 1))
	_ = b.inventory.SetItem(4, item.NewStack(item.BlazePowder{}, 1))
	brewTicks(w, b, 401)
	p, _ := b.inventory.Item(2)
	ingredient, _ := b.inventory.Item(0)
	if _, ok := ingredient.Item().(item.GlassBottle); !ok || p.Item() != (item.LingeringPotion{Type: potion.Healing()}) {
		t.Fatalf("ingredient %v, potion %v; want a glass bottle and a lingering potion of healing", ingredient.Item(), p.Item())
	}
	if !recipe.ValidBrewingReagent(item.Gunpowder{}) || !recipe.ValidBrewingReagent(item.DragonBreath{}) {
		t.Fatal("gunpowder and dragon's breath are no brewing reagents")
	}
}
