package session

import (
	"maps"
	"reflect"
	"slices"
	"sync"
	"testing"
	_ "unsafe"

	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// registerVanillaRecipes is the vanilla recipe registration a server runs
// on start.
//
//go:linkname registerVanillaRecipes github.com/df-mc/dragonfly/server/item/recipe.registerVanilla
func registerVanillaRecipes()

var vanillaRecipesOnce sync.Once

// TestRecipeUnlockPacketsUnchanged holds the recipe unlocks the game keeps
// to those the session accepted when it decided them. For the vanilla
// registry, every Java ID the session maps to a Bedrock recipe must be one
// the game keeps, recipe.KnownJavaID, and the packet the session sends for
// every ID the game knows must be the one it sends for those it maps: the
// IDs the game keeps beyond them, of kinds Bedrock does not show, send
// nothing.
func TestRecipeUnlockPacketsUnchanged(t *testing.T) {
	vanillaRecipesOnce.Do(func() {
		world.DefaultBlockRegistry.Finalize()
		registerVanillaRecipes()
	})
	var known []string
	for _, r := range recipe.Recipes() {
		known = append(known, recipe.JavaRecipeIDs(r)...)
	}
	slices.Sort(known)
	known = slices.Compact(known)

	s := newCaptureSession()
	s.br = world.DefaultBlockRegistry
	s.javaRecipeIDs = maps.Clone(recipesFor(s.br).javaIDs)
	mapped := slices.Sorted(maps.Keys(s.javaRecipeIDs))
	if len(mapped) == 0 {
		t.Fatal("the session maps no Java recipe ID")
	}
	for _, id := range mapped {
		if !recipe.KnownJavaID(id) {
			t.Errorf("the session maps %s, which the game does not keep", id)
		}
	}
	for _, id := range known {
		if !recipe.KnownJavaID(id) {
			t.Errorf("KnownJavaID(%s) = false for an ID of a registered recipe", id)
		}
	}
	if recipe.KnownJavaID("minecraft:not_a_recipe") {
		t.Error("KnownJavaID of an unknown ID = true")
	}
	t.Logf("the game knows %d Java recipe IDs, the session maps %d: %d kept beyond", len(known), len(mapped), len(known)-len(mapped))

	s.ViewRecipesUnlocked(known)
	got := s.taken()
	s.ViewRecipesUnlocked(mapped)
	want := s.taken()
	if len(want) != 1 || want[0].(*v2193.UnlockedRecipesPacket).UnlockType != v2193.UnlockedRecipesTypeNewlyUnlocked {
		t.Fatalf("the unlock of the mapped IDs sent %#v, want one packet of newly unlocked recipes", want)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the unlock of every known ID sent another packet than that of the mapped IDs")
	}
}
