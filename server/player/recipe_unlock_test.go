package player

import (
	"reflect"
	"slices"
	"sync"
	"testing"
	_ "unsafe"

	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/world"
)

// registerVanillaRecipes is the vanilla recipe registration a server runs
// on start.
//
//go:linkname registerVanillaRecipes github.com/df-mc/dragonfly/server/item/recipe.registerVanilla
func registerVanillaRecipes()

var vanillaRecipesOnce sync.Once

// TestUnlockRecipesKeepsKnownIDs checks that a player keeps the recipe
// unlocks of the IDs of registered recipes, whatever its client shows, and
// shows its client every ID passed, in order.
func TestUnlockRecipesKeepsKnownIDs(t *testing.T) {
	vanillaRecipesOnce.Do(func() {
		world.DefaultBlockRegistry.Finalize()
		registerVanillaRecipes()
	})
	ids := []string{"minecraft:torch", "minecraft:not_a_recipe", "minecraft:stick", "minecraft:torch"}
	for _, id := range []string{"minecraft:torch", "minecraft:stick"} {
		if !recipe.KnownJavaID(id) {
			t.Fatalf("%s is not the ID of a vanilla recipe", id)
		}
	}
	w, _ := clientTestWorld(t)
	r := clienttest.New()
	withClientPlayer(t, w, r, func(_ *world.Tx, p *Player) {
		r.Reset()
		p.UnlockRecipes(ids)
		if got, want := p.UnlockedRecipeIDs(), []string{"minecraft:stick", "minecraft:torch"}; !slices.Equal(got, want) {
			t.Errorf("UnlockedRecipeIDs() = %v, want %v", got, want)
		}
		calls := r.CallsTo("ViewRecipesUnlocked")
		if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []any{ids}) {
			t.Errorf("ViewRecipesUnlocked calls = %v, want one of %v", calls, ids)
		}
		if n := p.LockRecipes([]string{"minecraft:torch", "minecraft:not_a_recipe"}); n != 1 {
			t.Errorf("LockRecipes() = %d, want 1", n)
		}
	})
}
