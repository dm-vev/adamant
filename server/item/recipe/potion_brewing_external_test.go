package recipe_test

import (
	"testing"
	_ "unsafe"

	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/item/potion"
	"github.com/dm-vev/adamant/server/item/recipe"
	"github.com/dm-vev/adamant/server/world"
)

// registerVanilla is the vanilla recipe registration the server runs on
// start. It registers the recipes once per process, however often it is
// called.
//
//go:linkname registerVanilla github.com/dm-vev/adamant/server/item/recipe.registerVanilla
func registerVanilla()

func TestAwkwardPotionBrewsTrialPotions(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	registerVanilla()
	cases := []struct {
		reagent world.Item
		want    potion.Potion
	}{
		{item.BreezeRod{}, potion.WindCharging()},
		{block.Cobweb{}, potion.Weaving()},
		{block.Slime{}, potion.Oozing()},
		{block.Stone{}, potion.Infestation()},
	}
	for _, c := range cases {
		out, ok := recipe.Perform("brewing_stand", item.Potion{Type: potion.Awkward()}, c.reagent)
		if !ok || len(out) != 1 || out[0].Item() != (item.Potion{Type: c.want}) {
			t.Errorf("awkward potion with %T brews %v (%v), want %v", c.reagent, out, ok, c.want)
		}
	}
}
