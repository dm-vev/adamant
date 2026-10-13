package block

import (
	"image/color"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
)

// useOnCauldron activates the cauldron at pos with held and returns the
// resulting context.
func useOnCauldron(t *testing.T, tx *world.Tx, pos cube.Pos, held item.Stack) *item.UseContext {
	t.Helper()
	ctx := &item.UseContext{}
	if !tx.Block(pos).(Cauldron).Activate(pos, cube.FaceUp, tx, &candleTestUser{held: held}, ctx) {
		t.Fatalf("cauldron %#v rejected %v", tx.Block(pos), held)
	}
	return ctx
}

func TestCauldronBucketsAndBottlesMoveBedrockLevels(t *testing.T) {
	w := newMultifaceTestWorld(t)
	pos := cube.Pos{0, 1, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos, Cauldron{}, nil)
		ctx := useOnCauldron(t, tx, pos, item.NewStack(item.Bucket{Content: item.LiquidBucketContent(Water{})}, 1))
		if c := tx.Block(pos).(Cauldron); c.FillLevel != 6 || c.Liquid != CauldronWater {
			t.Fatalf("water bucket filled %#v, want a full water cauldron", c)
		}
		if b, ok := ctx.NewItem.Item().(item.Bucket); !ok || !b.Empty() {
			t.Fatalf("water bucket became %v, want an empty bucket", ctx.NewItem)
		}
		ctx = useOnCauldron(t, tx, pos, item.NewStack(item.GlassBottle{}, 1))
		if c := tx.Block(pos).(Cauldron); c.FillLevel != 4 {
			t.Fatalf("bottle left fill level %d, want 4", c.FillLevel)
		}
		if p, ok := ctx.NewItem.Item().(item.Potion); !ok || p.Type != potion.Water() {
			t.Fatalf("bottle filled with %v, want a water bottle", ctx.NewItem)
		}
		// An empty bucket only takes from a full cauldron.
		if tx.Block(pos).(Cauldron).Activate(pos, cube.FaceUp, tx, &candleTestUser{held: item.NewStack(item.Bucket{}, 1)}, &item.UseContext{}) {
			t.Fatal("empty bucket took from a partly filled cauldron")
		}
		useOnCauldron(t, tx, pos, item.NewStack(item.Potion{Type: potion.Water()}, 1))
		ctx = useOnCauldron(t, tx, pos, item.NewStack(item.Bucket{}, 1))
		if b, ok := ctx.NewItem.Item().(item.Bucket); !ok || b.Content.String() != "water" || tx.Block(pos).(Cauldron).FillLevel != 0 {
			t.Fatalf("full cauldron gave %v and kept %#v", ctx.NewItem, tx.Block(pos))
		}
	})
}

func TestCauldronMixingDifferentPotionsBoilsAway(t *testing.T) {
	w := newMultifaceTestWorld(t)
	pos := cube.Pos{0, 1, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos, Cauldron{}, nil)
		useOnCauldron(t, tx, pos, item.NewStack(item.Potion{Type: potion.Swiftness()}, 1))
		if c := tx.Block(pos).(Cauldron); c.FillLevel != 2 || c.PotionKind != CauldronPotion || c.Potion != potion.Swiftness() {
			t.Fatalf("swiftness potion gave %#v", c)
		}
		ctx := useOnCauldron(t, tx, pos, item.NewStack(item.SplashPotion{Type: potion.Swiftness()}, 1))
		if c := tx.Block(pos).(Cauldron); c.FillLevel != 0 {
			t.Fatalf("a splash potion into a drinkable potion left %#v, want an empty cauldron", c)
		}
		if _, ok := ctx.NewItem.Item().(item.GlassBottle); !ok {
			t.Fatalf("mixed potion left %v, want its glass bottle", ctx.NewItem)
		}
	})
}

func TestCauldronDyedWaterDyesAndWashesLeather(t *testing.T) {
	w := newMultifaceTestWorld(t)
	pos := cube.Pos{0, 1, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos, Cauldron{FillLevel: 6, Liquid: CauldronWater}, nil)
		useOnCauldron(t, tx, pos, item.NewStack(item.Dye{Colour: item.ColourRed()}, 1))
		red := item.ColourRed().RGBA()
		if c := tx.Block(pos).(Cauldron); c.DyeColour != red || c.FillLevel != 6 {
			t.Fatalf("red dye gave %#v", c)
		}
		useOnCauldron(t, tx, pos, item.NewStack(item.Dye{Colour: item.ColourBlue()}, 1))
		blue := item.ColourBlue().RGBA()
		mixed := color.RGBA{R: uint8((int(red.R) + int(blue.R)) / 2), G: uint8((int(red.G) + int(blue.G)) / 2), B: uint8((int(red.B) + int(blue.B)) / 2), A: 0xff}
		if c := tx.Block(pos).(Cauldron); c.DyeColour != mixed {
			t.Fatalf("mixed dye = %v, want %v", c.DyeColour, mixed)
		}
		ctx := useOnCauldron(t, tx, pos, item.NewStack(item.Boots{Tier: item.ArmourTierLeather{}}, 1))
		boots := ctx.NewItem.Item().(item.Boots)
		if boots.Tier.(item.ArmourTierLeather).Colour != mixed || !ctx.ReplaceHeldItem || tx.Block(pos).(Cauldron).FillLevel != 4 {
			t.Fatalf("dyed boots %v, cauldron %#v", boots, tx.Block(pos))
		}

		tx.SetBlock(pos, Cauldron{FillLevel: 3, Liquid: CauldronWater}, nil)
		ctx = useOnCauldron(t, tx, pos, item.NewStack(boots, 1))
		if washed := ctx.NewItem.Item().(item.Boots); washed.Tier.(item.ArmourTierLeather).Colour != (color.RGBA{}) || tx.Block(pos).(Cauldron).FillLevel != 2 {
			t.Fatalf("plain water left boots %v and cauldron %#v", washed, tx.Block(pos))
		}
	})
}

func TestCauldronTipsArrowsByStackSize(t *testing.T) {
	cases := []struct {
		fill, arrows, tipped, left int
	}{
		{fill: 6, arrows: 10, tipped: 10, left: 5},
		{fill: 6, arrows: 20, tipped: 20, left: 2},
		{fill: 6, arrows: 64, tipped: 64, left: 0},
		{fill: 2, arrows: 64, tipped: 16, left: 0},
	}
	for _, c := range cases {
		w := newMultifaceTestWorld(t)
		pos := cube.Pos{0, 1, 0}
		<-w.Exec(func(tx *world.Tx) {
			tx.SetBlock(pos, Cauldron{FillLevel: c.fill, Liquid: CauldronWater, PotionKind: CauldronPotion, Potion: potion.Poison()}, nil)
			ctx := useOnCauldron(t, tx, pos, item.NewStack(item.Arrow{}, c.arrows))
			arrow, ok := ctx.NewItem.Item().(item.Arrow)
			if !ok || arrow.Tip != potion.Poison() || ctx.NewItem.Count() != c.tipped || ctx.CountSub != c.tipped {
				t.Fatalf("fill %d with %d arrows: got %v, consumed %d", c.fill, c.arrows, ctx.NewItem, ctx.CountSub)
			}
			if got := tx.Block(pos).(Cauldron).FillLevel; got != c.left {
				t.Fatalf("fill %d with %d arrows left level %d, want %d", c.fill, c.arrows, got, c.left)
			}
		})
	}
}

func TestCauldronPrecipitationFillsMatchingContents(t *testing.T) {
	w := newMultifaceTestWorld(t)
	pos := cube.Pos{0, 1, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos, Cauldron{}, nil)
		for range 2000 {
			tx.Block(pos).(Cauldron).HandlePrecipitation(pos, tx, world.PrecipitationRain)
		}
		if c := tx.Block(pos).(Cauldron); c.Liquid != CauldronWater || c.FillLevel != 6 {
			t.Fatalf("rain filled %#v, want a full water cauldron", c)
		}
		tx.Block(pos).(Cauldron).HandlePrecipitation(pos, tx, world.PrecipitationSnow)
		lava := Cauldron{FillLevel: 6, Liquid: CauldronLava}
		tx.SetBlock(pos, lava, nil)
		for range 2000 {
			tx.Block(pos).(Cauldron).HandlePrecipitation(pos, tx, world.PrecipitationRain)
		}
		if c := tx.Block(pos).(Cauldron); c != lava {
			t.Fatalf("rain changed a lava cauldron into %#v", c)
		}
		tx.SetBlock(pos, Cauldron{}, nil)
		for range 2000 {
			tx.Block(pos).(Cauldron).HandlePrecipitation(pos, tx, world.PrecipitationSnow)
		}
		if c := tx.Block(pos).(Cauldron); c.Liquid != CauldronPowderSnow || c.FillLevel != 6 {
			t.Fatalf("snow filled %#v, want a full powder snow cauldron", c)
		}
	})
}

func TestCauldronActorKeepsPotionAndDye(t *testing.T) {
	dyed := Cauldron{FillLevel: 4, Liquid: CauldronWater, DyeColour: item.ColourLime().RGBA()}
	if got := (Cauldron{FillLevel: 4}).DecodeNBT(dyed.EncodeNBT()).(Cauldron); got != dyed {
		t.Fatalf("dyed cauldron reloaded as %#v", got)
	}
	brewed := Cauldron{FillLevel: 2, Liquid: CauldronWater, PotionKind: CauldronLingeringPotion, Potion: potion.Healing()}
	if got := (Cauldron{FillLevel: 2}).DecodeNBT(brewed.EncodeNBT()).(Cauldron); got != brewed {
		t.Fatalf("potion cauldron reloaded as %#v", got)
	}
}
