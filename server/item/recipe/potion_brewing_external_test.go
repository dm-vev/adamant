package recipe_test

import (
	"sync"
	"testing"
	_ "unsafe"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/world"
)

// registerVanilla is the vanilla recipe registration the server runs on
// start.
//
//go:linkname registerVanilla github.com/df-mc/dragonfly/server/item/recipe.registerVanilla
func registerVanilla()

var registerVanillaOnce sync.Once

func TestAwkwardPotionBrewsTrialPotions(t *testing.T) {
	registerVanillaOnce.Do(func() {
		world.DefaultBlockRegistry.Finalize()
		registerVanilla()
	})
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
