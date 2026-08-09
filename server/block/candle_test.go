package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
)

func init() {
	world.DefaultBlockRegistry.Finalize()
}

func worldFinaliseBlockRegistry() {
	world.DefaultBlockRegistry.Finalize()
}

type candleTestUser struct {
	pistonTestUser
	held item.Stack
}

func (u *candleTestUser) HeldItems() (item.Stack, item.Stack) { return u.held, item.Stack{} }

func TestCandleIgnitePreservesAdditionalCandles(t *testing.T) {
	w := world.Config{Generator: world.NopGenerator{}, Provider: world.NopProvider{}}.New()
	defer w.Close()

	pos := cube.Pos{0, 64, 0}

	done := w.Exec(func(tx *world.Tx) {
		original := Candle{AdditionalCandles: 3, Coloured: true, Colour: item.ColourRed()}
		tx.SetBlock(pos, original, nil)

		if ok := (Candle{}).Ignite(pos, tx, nil); !ok {
			t.Fatalf("expected ignite to succeed")
		}

		blk, ok := tx.Block(pos).(Candle)
		if !ok {
			t.Fatalf("expected candle block, got %T", tx.Block(pos))
		}
		if !blk.Lit {
			t.Fatalf("expected candle to be lit after ignition")
		}
		if blk.AdditionalCandles != original.AdditionalCandles {
			t.Fatalf("expected additional candles to be preserved, got %d", blk.AdditionalCandles)
		}
		if blk.Coloured != original.Coloured {
			t.Fatalf("expected coloured state to be preserved")
		}
		if blk.Colour != original.Colour {
			t.Fatalf("expected colour to be preserved, got %v", blk.Colour)
		}
	})

	<-done
}

func TestCandleSplashPreservesAdditionalCandles(t *testing.T) {
	w := world.Config{Generator: world.NopGenerator{}, Provider: world.NopProvider{}}.New()
	defer w.Close()

	pos := cube.Pos{0, 64, 0}

	done := w.Exec(func(tx *world.Tx) {
		original := Candle{AdditionalCandles: 2, Lit: true}
		tx.SetBlock(pos, original, nil)

		(Candle{}).Splash(tx, pos)

		blk, ok := tx.Block(pos).(Candle)
		if !ok {
			t.Fatalf("expected candle block, got %T", tx.Block(pos))
		}
		if blk.Lit {
			t.Fatalf("expected candle to be extinguished after splash")
		}
		if blk.AdditionalCandles != original.AdditionalCandles {
			t.Fatalf("expected additional candles to be preserved, got %d", blk.AdditionalCandles)
		}
	})

	<-done
}

func TestCandleActivateFireAspectExtinguishesLitCandle(t *testing.T) {
	w := world.Config{Generator: world.NopGenerator{}, Provider: world.NopProvider{}}.New()
	defer w.Close()

	pos := cube.Pos{0, 64, 0}
	user := &candleTestUser{held: item.NewStack(item.Sword{Tier: item.ToolTierWood}, 1).WithEnchantments(item.NewEnchantment(enchantment.FireAspect, 1))}
	<-w.Exec(func(tx *world.Tx) {
		candle := Candle{Lit: true}
		tx.SetBlock(pos, candle, nil)
		ctx := &item.UseContext{}
		if !candle.Activate(pos, cube.FaceUp, tx, user, ctx) {
			t.Fatal("expected lit candle activation to succeed")
		}
		if ctx.Damage != 0 {
			t.Fatalf("unexpected durability damage: %d", ctx.Damage)
		}
		if tx.Block(pos).(Candle).Lit {
			t.Fatal("expected candle to be extinguished")
		}
	})
}
