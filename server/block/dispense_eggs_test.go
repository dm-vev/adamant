package block_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
)

// TestDispenserThrowsEggsOfTheirClimate checks the behaviours of Items.EGG,
// Items.BLUE_EGG and Items.BROWN_EGG: each makes the egg of its own climate,
// whose chickens hatch of that climate.
func TestDispenserThrowsEggsOfTheirClimate(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		variant item.EggVariant
		climate string
	}{
		{item.EggTemperate, "temperate"},
		{item.EggBlue, "cold"},
		{item.EggBrown, "warm"},
	} {
		t.Run(c.climate, func(t *testing.T) {
			t.Parallel()
			dt := newDispenserTest(t, cube.FaceEast)
			dt.dispense(nil, item.NewStack(item.Egg{Variant: c.variant}, 1))
			eggs := dt.entities("minecraft:egg")
			if len(eggs) != 1 {
				t.Fatalf("%d eggs thrown, want one", len(eggs))
			}
			climate := eggs[0].(*entity.Ent).Behaviour().(interface{ ClimateVariant() string }).ClimateVariant()
			if climate != c.climate {
				t.Fatalf("the egg is %s, want %s", climate, c.climate)
			}
		})
	}
}
