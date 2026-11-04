package menu_test

import (
	"errors"
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// tradeViewer records the offers a menu shows.
type tradeViewer struct {
	fakeViewer
	trades [][]entity.MerchantOffer
}

func (v *tradeViewer) ViewTrades(_ *menu.Menu, _ entity.Merchant, offers []entity.MerchantOffer) {
	v.trades = append(v.trades, offers)
}

// The offers of the villager of the trade tests, by index.
const (
	tradeBread   = iota // six bread for an emerald, priced at two by demand
	tradeSword          // an iron sword for an emerald and a stick
	tradeUsedUp         // an apple, used up
	tradeLastUse        // a cookie for an emerald, once more
)

// cancelTake is an inventory handler that cancels every take.
type cancelTake struct{ inventory.NopHandler }

func (cancelTake) HandleTake(ctx *inventory.Context, _ int, _ item.Stack) { ctx.Cancel() }

// tradeTest runs f with a batch of a player trading with a villager of the
// offers above, over fresh inventories.
func tradeTest(t *testing.T, f func(tx *world.Tx, b *menu.Tx, inv menu.Inventories, v *tradeViewer, villager entity.Merchant)) {
	t.Helper()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	offer := func(buy, buyB, sell item.Stack, uses, maxUses, demand int) map[string]any {
		o := map[string]any{
			"buy": item.WriteNBT(buy), "sell": item.WriteNBT(sell),
			"uses": int32(uses), "maxUses": int32(maxUses), "xp": int32(0), "rewardExp": byte(0),
			"demand": int32(demand), "priceMultiplier": float32(0.05),
		}
		if !buyB.Empty() {
			o["buyB"] = item.WriteNBT(buyB)
		}
		return o
	}
	emerald := item.NewStack(item.Emerald{}, 1)
	<-w.Exec(func(tx *world.Tx) {
		p := tx.AddEntity(world.EntitySpawnOpts{}.New(player.Type, player.Config{Name: "Trader", Position: mgl64.Vec3{0.5, 65, 0.5}})).(*player.Player)
		h, err := entity.NewFromStructureNBT(map[string]any{
			"id":           "minecraft:villager",
			"NoAI":         byte(1),
			"VillagerData": map[string]any{"type": "minecraft:plains", "profession": "minecraft:farmer", "level": int32(1)},
			"Offers": map[string]any{"Recipes": []any{
				tradeBread:   offer(emerald, item.Stack{}, item.NewStack(item.Bread{}, 6), 0, 16, 20),
				tradeSword:   offer(emerald, item.NewStack(item.Stick{}, 1), item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1), 0, 16, 0),
				tradeUsedUp:  offer(emerald, item.Stack{}, item.NewStack(item.Apple{}, 1), 16, 16, 0),
				tradeLastUse: offer(emerald, item.Stack{}, item.NewStack(item.Cookie{}, 1), 3, 4, 0),
			}},
		}, world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 65, 3.5}})
		if err != nil {
			t.Fatal(err)
		}
		e := tx.AddEntity(h)
		villager := e.(entity.Merchant)
		m, ok := menu.OpenMerchant(e, villager, p.H())
		if !ok {
			t.Fatal("the villager does not trade")
		}
		inv := menu.Inventories{
			Main: inventory.New(36, nil), OffHand: inventory.New(1, nil), EnderChest: inventory.New(27, nil),
			UI: inventory.New(54, nil), Armour: inventory.NewArmour(nil),
		}
		v := &tradeViewer{}
		b := menu.Begin(menu.Opts{Tx: tx, Owner: p, Inv: inv, Menu: m, Viewer: v, Name: testName})
		f(tx, b, inv, v, villager)
	})
}

// TestTradeIntoASlotThatRefuses checks the result of a trade put in a slot
// that does not take it, as bread in the off hand of a player: taken from
// the offer (TakeTrade), it is refused with ErrSlotRefusesItem before the
// trade is made, and nothing is paid, used or changed; made with the inputs
// (Trade), the result is refused at the move, and the rollback gives the
// inputs back. The off hand left its slot as it was while the trade went
// on: the bread was paid for and vanished. BDS 1.26.52.3 refuses such a
// request (status 50).
func TestTradeIntoASlotThatRefuses(t *testing.T) {
	emeralds := func(n int) item.Stack { return item.NewStack(item.Emerald{}, n) }
	shieldsOnly := func(s item.Stack, _ int) bool {
		_, shield := s.Item().(item.Shield)
		return s.Empty() || shield
	}
	tradeTest(t, func(_ *world.Tx, b *menu.Tx, inv menu.Inventories, v *tradeViewer, villager entity.Merchant) {
		inv.OffHand.SlotValidatorFunc(shieldsOnly)
		_ = inv.Main.SetItem(0, emeralds(5))
		offHand := menu.Ref{Slot: menu.Slot{Inv: inv.OffHand}}
		if err := b.TakeTrade(tradeBread, 6, offHand); !errors.Is(err, menu.ErrSlotRefusesItem) {
			t.Fatalf("bread taken into the off hand: %v, want ErrSlotRefusesItem", err)
		}
		if got := uses(t, villager, tradeBread); got != 0 {
			t.Errorf("the offer was used %d times", got)
		}
		if got, _ := inv.Main.Item(0); !got.Equal(emeralds(5)) {
			t.Errorf("the inventory holds %v, want the five emeralds", got)
		}
		if n := len(b.Changes()); n != 0 || len(v.trades) != 0 {
			t.Errorf("the refused trade made %d changes and showed %d offers", n, len(v.trades))
		}

		_ = inv.UI.SetItem(menu.UITradeInputA, emeralds(5))
		if err := b.Trade(tradeBread, 1); err != nil {
			t.Fatal(err)
		}
		result := menu.Ref{Slot: menu.Slot{Inv: inv.UI, Index: menu.UIResult}}
		if err := b.Move(result, offHand, 6); !errors.Is(err, menu.ErrSlotRefusesItem) {
			t.Fatalf("the bread of the trade moved to the off hand: %v, want ErrSlotRefusesItem", err)
		}
		b.Rollback()
		if got, _ := inv.UI.Item(menu.UITradeInputA); !got.Equal(emeralds(5)) {
			t.Errorf("the input holds %v after the rollback, want the five emeralds", got)
		}
		if got, _ := inv.OffHand.Item(0); !got.Empty() {
			t.Errorf("the off hand holds %v", got)
		}
	})
}

// uses returns the uses of offer index of merchant.
func uses(t *testing.T, merchant entity.Merchant, index int) int {
	t.Helper()
	offers, err := merchant.MerchantOffers()
	if err != nil {
		t.Fatal(err)
	}
	return offers[index].Uses
}

// TestTrade checks trades through the inputs of a merchant menu: once and
// repeated, priced as CostA prices them, with a second cost, and those the
// merchant refuses.
func TestTrade(t *testing.T) {
	emeralds := func(n int) item.Stack { return item.NewStack(item.Emerald{}, n) }
	tradeInput := testName(menu.Slot{Index: menu.UITradeInputA}, menu.RoleTradeInput)
	tradeInputB := testName(menu.Slot{Index: menu.UITradeInputB}, menu.RoleTradeInput)
	result := testName(menu.Slot{Index: menu.UIResult}, menu.RoleResult)

	t.Run("bread twice, at two emeralds each", func(t *testing.T) {
		tradeTest(t, func(_ *world.Tx, b *menu.Tx, inv menu.Inventories, v *tradeViewer, villager entity.Merchant) {
			_ = inv.UI.SetItem(menu.UITradeInputA, emeralds(5))
			if err := b.Trade(tradeBread, 2); err != nil {
				t.Fatal(err)
			}
			changes := b.Changes()
			if len(changes) != 3 || changes[0].Ref.Name != tradeInput || changes[1].Ref.Name != tradeInputB || changes[2].Ref.Name != result {
				t.Fatalf("the changes are %v, want input A, input B and the result", changes)
			}
			if got := changes[0].After.Count(); got != 1 {
				t.Errorf("%d emeralds left, want 1", got)
			}
			if res, _ := inv.UI.Item(menu.UIResult); !res.Equal(item.NewStack(item.Bread{}, 12)) {
				t.Errorf("the result is %v, want 12 bread", res)
			}
			if n := uses(t, villager, tradeBread); n != 2 {
				t.Errorf("the offer has %d uses, want 2", n)
			}
			if len(v.trades) != 1 || v.trades[0][tradeBread].Uses != 2 {
				t.Errorf("the offers shown after the trade are %v", v.trades)
			}
		})
	})
	t.Run("a sword for an emerald and a stick", func(t *testing.T) {
		tradeTest(t, func(_ *world.Tx, b *menu.Tx, inv menu.Inventories, _ *tradeViewer, _ entity.Merchant) {
			_ = inv.UI.SetItem(menu.UITradeInputA, emeralds(1))
			_ = inv.UI.SetItem(menu.UITradeInputB, item.NewStack(item.Stick{}, 3))
			if err := b.Trade(tradeSword, 1); err != nil {
				t.Fatal(err)
			}
			if a, _ := inv.UI.Item(menu.UITradeInputA); !a.Empty() {
				t.Errorf("input A holds %v", a)
			}
			if sticks, _ := inv.UI.Item(menu.UITradeInputB); sticks.Count() != 2 {
				t.Errorf("input B holds %v, want 2 sticks", sticks)
			}
		})
	})
	for _, test := range []struct {
		name    string
		a, b    item.Stack
		offer   int
		times   int
		usesNow int // the uses of the offer after the refusal
	}{
		{name: "bread for one emerald, priced at two", a: emeralds(1), offer: tradeBread, times: 1},
		{name: "bread twice for three emeralds", a: emeralds(3), offer: tradeBread, times: 2, usesNow: 1},
		{name: "a sword without its stick", a: emeralds(1), offer: tradeSword, times: 1},
		{name: "two swords, which do not stack", a: emeralds(2), b: item.NewStack(item.Stick{}, 2), offer: tradeSword, times: 2, usesNow: 1},
		{name: "the used-up offer", a: emeralds(1), offer: tradeUsedUp, times: 1, usesNow: 16},
		{name: "two cookies of the last use", a: emeralds(2), offer: tradeLastUse, times: 2, usesNow: 4},
		{name: "an offer out of range", a: emeralds(1), offer: 9, times: 1},
		{name: "an offer of index -1", a: emeralds(1), offer: -1, times: 1},
		{name: "a locked emerald", a: emeralds(1).WithLockMode(item.LockInInventory), b: item.NewStack(item.Stick{}, 1), offer: tradeSword, times: 1},
	} {
		t.Run("refused: "+test.name, func(t *testing.T) {
			tradeTest(t, func(_ *world.Tx, b *menu.Tx, inv menu.Inventories, v *tradeViewer, villager entity.Merchant) {
				_ = inv.UI.SetItem(menu.UITradeInputA, test.a)
				_ = inv.UI.SetItem(menu.UITradeInputB, test.b)
				if err := b.Trade(test.offer, test.times); err == nil {
					t.Fatal("the trade was made")
				}
				if len(b.Changes()) != 0 || len(v.trades) != 0 {
					t.Errorf("a refusal made %d changes and showed %d offers", len(b.Changes()), len(v.trades))
				}
				// A trade the merchant completed before a later one failed
				// keeps its use, as before.
				if test.offer >= 0 && test.offer < 4 {
					if n := uses(t, villager, test.offer); n != test.usesNow {
						t.Errorf("the offer has %d uses, want %d", n, test.usesNow)
					}
				}
			})
		})
	}
	t.Run("refused: no merchant menu", func(t *testing.T) {
		w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
		t.Cleanup(func() { _ = w.Close() })
		<-w.Exec(func(tx *world.Tx) {
			b, _ := testBatch(tx, nil, nil)
			if err := b.Trade(0, 1); err == nil {
				t.Error("a trade was made with no menu")
			}
			if err := b.TakeTrade(0, 6, menu.Ref{}); err == nil {
				t.Error("a trade was taken with no menu")
			}
		})
	})
}

// TestTakeTrade checks the results of offers taken, paid from the main
// inventory, and those the merchant refuses.
func TestTakeTrade(t *testing.T) {
	emeralds := func(n int) item.Stack { return item.NewStack(item.Emerald{}, n) }
	t.Run("bread paid from two slots onto bread", func(t *testing.T) {
		tradeTest(t, func(_ *world.Tx, b *menu.Tx, inv menu.Inventories, v *tradeViewer, villager entity.Merchant) {
			_ = inv.Main.SetItem(0, emeralds(1))
			_ = inv.Main.SetItem(3, emeralds(4))
			_ = inv.Main.SetItem(7, item.NewStack(item.Bread{}, 10))
			if err := b.TakeTrade(tradeBread, 6, ref(inv.Main, 7, 1007)); err != nil {
				t.Fatal(err)
			}
			want := []struct {
				name  menu.Name
				count int
			}{
				{testName(menu.Slot{Index: 0}, menu.RoleInventory), 0},
				{testName(menu.Slot{Index: 3}, menu.RoleInventory), 3},
				{1007, 16},
			}
			changes := b.Changes()
			if len(changes) != len(want) {
				t.Fatalf("%d changes, want %d", len(changes), len(want))
			}
			for i, c := range changes {
				if c.Ref.Name != want[i].name || c.After.Count() != want[i].count {
					t.Errorf("change %d: %v of %d, want %v of %d", i, c.Ref.Name, c.After.Count(), want[i].name, want[i].count)
				}
			}
			if n := uses(t, villager, tradeBread); n != 1 {
				t.Errorf("the offer has %d uses, want 1", n)
			}
			if len(v.trades) != 1 {
				t.Errorf("the offers were shown %d times, want once", len(v.trades))
			}
		})
	})
	t.Run("a sword into the slot of the stick paid", func(t *testing.T) {
		tradeTest(t, func(_ *world.Tx, b *menu.Tx, inv menu.Inventories, _ *tradeViewer, _ entity.Merchant) {
			_ = inv.Main.SetItem(0, emeralds(1))
			_ = inv.Main.SetItem(1, item.NewStack(item.Stick{}, 1))
			if err := b.TakeTrade(tradeSword, 1, ref(inv.Main, 1, 1001)); err != nil {
				t.Fatal(err)
			}
			if got, _ := inv.Main.Item(1); !got.Equal(item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1)) {
				t.Errorf("slot 1 holds %v, want the sword", got)
			}
		})
	})
	for _, test := range []struct {
		name  string
		main  map[int]item.Stack
		offer int
		count int
		to    int // a slot of the main inventory, or -1 for the UI result slot
		take  bool
	}{
		{name: "bread of the wrong count", main: map[int]item.Stack{0: emeralds(2)}, offer: tradeBread, count: 5, to: 5},
		{name: "bread for one emerald, priced at two", main: map[int]item.Stack{0: emeralds(1)}, offer: tradeBread, count: 6, to: 5},
		{name: "a sword without a stick", main: map[int]item.Stack{0: emeralds(1)}, offer: tradeSword, count: 1, to: 5},
		{name: "the used-up offer", main: map[int]item.Stack{0: emeralds(1)}, offer: tradeUsedUp, count: 1, to: 5},
		{name: "an offer out of range", main: map[int]item.Stack{0: emeralds(1)}, offer: 9, count: 1, to: 5},
		{name: "bread onto apples", main: map[int]item.Stack{0: emeralds(2), 5: item.NewStack(item.Apple{}, 1)}, offer: tradeBread, count: 6, to: 5},
		{name: "bread onto 60 bread", main: map[int]item.Stack{0: emeralds(2), 5: item.NewStack(item.Bread{}, 60)}, offer: tradeBread, count: 6, to: 5},
		{name: "bread into a slot out of range", main: map[int]item.Stack{0: emeralds(2)}, offer: tradeBread, count: 6, to: 40},
		{name: "bread onto a cookie in the UI", main: map[int]item.Stack{0: emeralds(2)}, offer: tradeBread, count: 6, to: -1},
		{name: "a take the inventory cancels", main: map[int]item.Stack{0: emeralds(2)}, offer: tradeBread, count: 6, to: 5, take: true},
	} {
		t.Run("refused: "+test.name, func(t *testing.T) {
			tradeTest(t, func(_ *world.Tx, b *menu.Tx, inv menu.Inventories, v *tradeViewer, villager entity.Merchant) {
				for slot, it := range test.main {
					_ = inv.Main.SetItem(slot, it)
				}
				if test.take {
					inv.Main.Handle(cancelTake{})
				}
				to := ref(inv.Main, test.to, 1)
				if test.to < 0 {
					_ = inv.UI.SetItem(menu.UIResult, item.NewStack(item.Cookie{}, 1))
					to = ref(inv.UI, menu.UIResult, 1)
				}
				if err := b.TakeTrade(test.offer, test.count, to); err == nil {
					t.Fatal("the trade was taken")
				}
				if len(b.Changes()) != 0 || len(v.trades) != 0 {
					t.Errorf("a refusal made %d changes and showed %d offers", len(b.Changes()), len(v.trades))
				}
				if test.offer == tradeBread {
					if n := uses(t, villager, tradeBread); n != 0 {
						t.Errorf("the offer has %d uses after a refusal", n)
					}
				}
			})
		})
	}
}
