package item_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
)

func TestStackFromJavaNBTResolvesRenamedItems(t *testing.T) {
	is := func(want world.Item) func(world.Item) bool {
		return func(got world.Item) bool { return got == want }
	}
	cases := []struct {
		data  map[string]any
		check func(world.Item) bool
	}{
		{map[string]any{"id": "minecraft:cobweb"}, is(block.Web{})},
		{map[string]any{"id": "minecraft:lily_pad"}, is(block.LilyPad{})},
		{map[string]any{"id": "minecraft:sweet_berries"}, is(block.SweetBerries{})},
		{map[string]any{"id": "minecraft:map"}, is(item.EmptyMap{})},
		{map[string]any{"id": "minecraft:red_bed"}, func(got world.Item) bool {
			bed, ok := got.(block.Bed)
			return ok && bed.Colour == item.ColourRed()
		}},
		{map[string]any{"id": "minecraft:light_gray_banner"}, func(got world.Item) bool {
			banner, ok := got.(block.Banner)
			return ok && banner.Colour == item.ColourLightGrey()
		}},
		// Bedrock has no spectral arrows; its loot gives plain arrows.
		{map[string]any{"id": "minecraft:spectral_arrow"}, is(item.Arrow{})},
		{map[string]any{"id": "minecraft:tipped_arrow", "components": map[string]any{"minecraft:potion_contents": map[string]any{"potion": "minecraft:strong_poison"}}}, is(item.Arrow{Tip: potion.StrongPoison()})},
		{map[string]any{"id": "minecraft:splash_potion", "components": map[string]any{"minecraft:potion_contents": "minecraft:long_slow_falling"}}, is(item.SplashPotion{Type: potion.LongSlowFalling()})},
	}
	for _, c := range cases {
		got, err := item.StackFromStructureNBT(c.data)
		if err != nil {
			t.Fatalf("%v: %v", c.data["id"], err)
		}
		if !c.check(got.Item()) {
			t.Fatalf("%v decoded to %#v", c.data["id"], got.Item())
		}
	}
}
