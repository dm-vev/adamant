package entity_test

import (
	"testing"
	_ "unsafe"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// traderTestWorld returns a world with a wide grass floor at y 63.
func traderTestWorld(t *testing.T) *world.World {
	t.Helper()
	w := newDeathTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		for x := -20; x <= 20; x++ {
			for z := -20; z <= 20; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Grass{}, nil)
			}
		}
	})
	return w
}

func addTrader(tx *world.Tx, pos mgl64.Vec3) *entity.WanderingTrader {
	return tx.AddEntity(entity.NewWanderingTrader(world.EntitySpawnOpts{Position: pos})).(*entity.WanderingTrader)
}

func trader(tx *world.Tx, h *world.EntityHandle) *entity.WanderingTrader {
	e, ok := h.Entity(tx)
	if !ok {
		return nil
	}
	return e.(*entity.WanderingTrader)
}

func addSurvivalPlayer(tx *world.Tx, pos mgl64.Vec3) *player.Player {
	return tx.AddEntity((world.EntitySpawnOpts{}).New(player.Type, player.Config{Position: pos, GameMode: world.GameModeSurvival})).(*player.Player)
}

func TestWanderingTraderOffersItsTradeSets(t *testing.T) {
	w := traderTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		tr := addTrader(tx, mgl64.Vec3{.5, 64, .5})
		offers, err := tr.MerchantOffers()
		if err != nil {
			t.Fatal(err)
		}
		// Two buying, two uncommon and five common offers.
		if len(offers) != 9 {
			t.Fatalf("trader has %d offers, want 9", len(offers))
		}
		for i, o := range offers {
			if o.Tier != 0 {
				t.Errorf("offer %d has tier %d, want the single tier 0", i, o.Tier)
			}
			_, sellsEmerald := o.Output.Item().(item.Emerald)
			_, wantsEmerald := o.Input.Item().(item.Emerald)
			if i < 2 && !sellsEmerald {
				t.Errorf("buying offer %d gives %v, want emeralds", i, o.Output)
			}
			if i >= 2 && !wantsEmerald {
				t.Errorf("selling offer %d wants %v, want emeralds", i, o.Input)
			}
		}
		again, _ := tr.MerchantOffers()
		for i := range offers {
			if !again[i].Output.Equal(offers[i].Output) {
				t.Fatalf("offers changed between calls")
			}
		}
		if tr.MerchantName() != "entity.wandering_trader.name" {
			t.Errorf("trade screen title %q", tr.MerchantName())
		}
	})
}

func TestWanderingTraderTradesUseOffersAndDropExperience(t *testing.T) {
	w := traderTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		tr := addTrader(tx, mgl64.Vec3{.5, 64, .5})
		p := addSurvivalPlayer(tx, mgl64.Vec3{2.5, 64, .5})
		offers, err := tr.MerchantOffers()
		if err != nil {
			t.Fatal(err)
		}
		for range offers[2].MaxUses {
			if ok, err := tr.CompleteMerchantTrade(tx, p, 2); !ok || err != nil {
				t.Fatalf("trade rejected: %v", err)
			}
		}
		if ok, _ := tr.CompleteMerchantTrade(tx, p, 2); ok {
			t.Fatal("exhausted offer was traded")
		}
		after, _ := tr.MerchantOffers()
		if after[2].Uses != offers[2].MaxUses {
			t.Fatalf("offer used %d times, want %d", after[2].Uses, offers[2].MaxUses)
		}
		xp, _ := deathDrops(tx)
		if xp < 3*offers[2].MaxUses || xp > 6*offers[2].MaxUses {
			t.Fatalf("%d trades dropped %d experience, want 3-6 each", offers[2].MaxUses, xp)
		}
	})
}

func TestWanderingTraderLeavesWhenItsDespawnDelayEnds(t *testing.T) {
	w := traderTestWorld(t)
	var leaving, trading *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		p := addSurvivalPlayer(tx, mgl64.Vec3{8.5, 64, .5})
		a := addTrader(tx, mgl64.Vec3{.5, 64, .5})
		a.SetDespawnDelay(3)
		b := addTrader(tx, mgl64.Vec3{8.5, 64, 2.5})
		b.SetDespawnDelay(3)
		if !b.StartTrading(p.H()) {
			t.Fatal("trader refused a free trade")
		}
		if b.StartTrading(addSurvivalPlayer(tx, mgl64.Vec3{8.5, 64, 4.5}).H()) {
			t.Fatal("trader took a second customer")
		}
		leaving, trading = a.H(), b.H()
	})
	for range 3 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if trader(tx, leaving) != nil {
			t.Fatal("trader stayed after its despawn delay")
		}
		tr := trader(tx, trading)
		if tr == nil || tr.DespawnDelay() != 3 {
			t.Fatal("trader left in the middle of a trade")
		}
	})
}

func TestWanderingTraderDrinksInvisibilityAtNightAndMilkAtDay(t *testing.T) {
	w := traderTestWorld(t)
	w.SetTime(18000)
	var h *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) { h = addTrader(tx, mgl64.Vec3{.5, 64, .5}).H() })
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		held, _ := trader(tx, h).HeldItems()
		if p, ok := held.Item().(item.Potion); !ok || p.Type != potion.Invisibility() || !trader(tx, h).UsingItem() {
			t.Fatalf("trader holds %v at night, want to drink invisibility", held)
		}
	})
	// The trader drinks for 32 ticks, and UseItemGoal empties its hand the
	// next tick the goal selector checks its running goals.
	for range 34 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		tr := trader(tx, h)
		if held, _ := tr.HeldItems(); !held.Empty() || tr.UsingItem() {
			t.Fatalf("trader still holds %v after drinking", held)
		}
		if !hasEffect(tr, effect.Invisibility) {
			t.Fatal("trader is visible after drinking invisibility")
		}
	})
	w.SetTime(6000)
	for range 34 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if hasEffect(trader(tx, h), effect.Invisibility) {
			t.Fatal("trader stays invisible in daylight")
		}
	})
}

func hasEffect(l entity.Living, t effect.Type) bool {
	for _, e := range l.Effects() {
		if e.Type() == t {
			return true
		}
	}
	return false
}

func TestWanderingTraderPanicsWhenHurt(t *testing.T) {
	w := traderTestWorld(t)
	var h *world.EntityHandle
	start := mgl64.Vec3{.5, 64, .5}
	<-w.Exec(func(tx *world.Tx) { h = addTrader(tx, start).H() })
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		p := addSurvivalPlayer(tx, mgl64.Vec3{1.5, 64, .5})
		trader(tx, h).Hurt(1, entity.AttackDamageSource{Attacker: p})
	})
	// Panicking traders run to random spots nearby at panic speed, so measure
	// how fast they run rather than how far they ended up.
	fastest, last := 0.0, start
	for range 40 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			pos := trader(tx, h).Position()
			pos[1], last[1] = 0, 0
			fastest, last = max(fastest, pos.Sub(last).Len()), pos
		})
	}
	// PanicGoal runs at half the movement speed of 0.7, which settles at
	// 0.26 blocks a tick; a strolling trader walks at most 0.13.
	if fastest < .2 {
		t.Fatalf("hurt trader ran at most %.3f blocks a tick, want it to panic", fastest)
	}
}

func TestWanderingTraderFleesFromZombies(t *testing.T) {
	w := traderTestWorld(t)
	var h *world.EntityHandle
	zombieAt := mgl64.Vec3{5.5, 64, .5}
	<-w.Exec(func(tx *world.Tx) {
		h = addTrader(tx, mgl64.Vec3{.5, 64, .5}).H()
		addMob(t, tx, "minecraft:zombie", zombieAt)
	})
	for range 60 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if d := trader(tx, h).Position().Sub(zombieAt).Len(); d < 8 {
			t.Fatalf("trader is %.2f blocks from where the zombie appeared, want it to flee", d)
		}
	})
}

func TestWanderingTraderWalksToItsWanderTarget(t *testing.T) {
	w := traderTestWorld(t)
	var h *world.EntityHandle
	target := cube.Pos{14, 64, 0}
	<-w.Exec(func(tx *world.Tx) {
		tr := addTrader(tx, mgl64.Vec3{.5, 64, .5})
		tr.SetWanderTarget(target)
		h = tr.H()
	})
	// The trader forgets its target once it gets within two blocks of it,
	// after which it may stroll off again.
	for tick := range 300 {
		w.AdvanceTick()
		arrived := false
		<-w.Exec(func(tx *world.Tx) {
			tr := trader(tx, h)
			if _, ok := tr.WanderTarget(); !ok {
				arrived = true
				if d := tr.Position().Sub(target.Vec3Centre()).Len(); d > 2.5 {
					t.Fatalf("trader dropped its wander target %.2f blocks away", d)
				}
			}
		})
		if arrived {
			if tick > 150 {
				t.Fatalf("trader took %d ticks to walk 14 blocks", tick)
			}
			return
		}
	}
	t.Fatal("trader never reached its wander target")
}

func addTraderLlama(t *testing.T, tx *world.Tx, tr *entity.WanderingTrader, pos mgl64.Vec3) *world.EntityHandle {
	t.Helper()
	llama := tx.AddEntity(entity.NewTraderLlama(world.EntitySpawnOpts{Position: pos}))
	if !llama.(entity.Leashable).AttachLeash(tr, tx) {
		t.Fatal("llama refused the trader's lead")
	}
	return llama.H()
}

func TestTraderLlamasLeaveWithTheirTrader(t *testing.T) {
	w := traderTestWorld(t)
	var led, free *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		tr := addTrader(tx, mgl64.Vec3{.5, 64, .5})
		tr.SetDespawnDelay(5)
		led = addTraderLlama(t, tx, tr, mgl64.Vec3{2.5, 64, .5})
		free = tx.AddEntity(entity.NewTraderLlama(world.EntitySpawnOpts{Position: mgl64.Vec3{-6.5, 64, .5}})).H()
	})
	for range 6 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if _, ok := led.Entity(tx); ok {
			t.Fatal("trader llama stayed after its trader left")
		}
		if _, ok := free.Entity(tx); !ok {
			t.Fatal("free trader llama left long before its own delay")
		}
	})
}

func TestTraderLlamasCannotBeRiddenWhileLed(t *testing.T) {
	w := traderTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		tr := addTrader(tx, mgl64.Vec3{.5, 64, .5})
		h := addTraderLlama(t, tx, tr, mgl64.Vec3{2.5, 64, .5})
		p := addSurvivalPlayer(tx, mgl64.Vec3{3.5, 64, .5})
		llama, _ := h.Entity(tx)
		if llama.(entity.Interactable).Interact(tx, p, &item.UseContext{}) {
			t.Fatal("player interacted with a llama the trader leads")
		}
		llama.(entity.Leashable).DetachLeash(tx, false)
		if !llama.(entity.Interactable).Interact(tx, p, &item.UseContext{}) {
			t.Fatal("player could not interact with a free trader llama")
		}
	})
}

// TestTraderLlamasSpitAtTheirTradersAttacker checks
// TraderLlamaDefendWanderingTraderGoal with RangedAttackGoal, which spits
// two seconds after the llama took aim.
func TestTraderLlamasSpitAtTheirTradersAttacker(t *testing.T) {
	w := traderTestWorld(t)
	var tr *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		trd := addTrader(tx, mgl64.Vec3{.5, 64, .5})
		// Llamas leave at once with a trader that never leaves.
		trd.SetDespawnDelay(48000)
		addTraderLlama(t, tx, trd, mgl64.Vec3{2.5, 64, .5})
		tr = trd.H()
	})
	for range 3 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if n := countEntities(tx, "minecraft:llama_spit"); n != 0 {
			t.Fatalf("llama spat %d times without a threat", n)
		}
		p := addSurvivalPlayer(tx, mgl64.Vec3{-3.5, 64, .5})
		trader(tx, tr).Hurt(1, entity.AttackDamageSource{Attacker: p})
	})
	spat := false
	for range 60 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) { spat = spat || countEntities(tx, "minecraft:llama_spit") != 0 })
	}
	if !spat {
		t.Fatal("trader llama did not spit at the trader's attacker")
	}
}

//go:linkname spawnWanderingTrader github.com/df-mc/dragonfly/server/entity.spawnWanderingTrader
func spawnWanderingTrader(tx *world.Tx, random *vanilla.Random) bool

func TestWanderingTradersSpawnAtTheVillageBellWithLlamas(t *testing.T) {
	w := newDeathTestWorld(t)
	bell := cube.Pos{10, 64, 0}
	<-w.Exec(func(tx *world.Tx) {
		for x := -70; x <= 70; x++ {
			for z := -70; z <= 70; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Grass{}, nil)
			}
		}
		tx.SetBlock(bell, block.Bell{}, nil)
		addSurvivalPlayer(tx, mgl64.Vec3{.5, 64, .5})
	})
	<-w.Exec(func(tx *world.Tx) {
		for seed := int64(0); seed < 1000; seed++ {
			random := vanilla.NewLegacyRandom(seed)
			if !spawnWanderingTrader(tx, &random) {
				continue
			}
			var tr *entity.WanderingTrader
			for e := range tx.Entities() {
				if found, ok := e.(*entity.WanderingTrader); ok {
					tr = found
				}
			}
			if tr == nil {
				t.Fatal("spawn reported success without a trader")
			}
			if tr.DespawnDelay() != 48000 {
				t.Errorf("trader leaves after %d ticks, want 48000", tr.DespawnDelay())
			}
			if target, ok := tr.WanderTarget(); !ok || target != bell {
				t.Errorf("trader wanders to %v, want the bell at %v", target, bell)
			}
			if home, radius, ok := tr.Home(); !ok || home != bell || radius != 16 {
				t.Errorf("trader stays %d blocks around %v, want 16 around the bell", radius, home)
			}
			if d := tr.Position().Sub(bell.Vec3Middle()); d[1] != 0 || d.Len() > 48*1.5 {
				t.Errorf("trader spawned at %v, want on the floor near the bell", tr.Position())
			}
			llamas := 0
			for e := range tx.Entities() {
				if e.H().Type() == entity.TraderLlamaType {
					llamas++
					if e.(interface{ HolderLeashID() int64 }).HolderLeashID() != tr.H().UniqueID() {
						t.Errorf("trader llama at %v is not on the trader's lead", e.Position())
					}
				}
			}
			if llamas == 0 {
				t.Error("trader arrived without llamas on open ground")
			}
			return
		}
		t.Fatal("no trader spawned in a thousand attempts")
	})
}

// TestWanderingTraderStandsStillWhileTrading checks TradeWithPlayerGoal: a
// trader holds still while a player trades with it, and a hurt trader ends
// the trade and walks on.
func TestWanderingTraderStandsStillWhileTrading(t *testing.T) {
	w := traderTestWorld(t)
	var h, customer *world.EntityHandle
	start := mgl64.Vec3{.5, 64, .5}
	<-w.Exec(func(tx *world.Tx) {
		tr := addTrader(tx, start)
		tr.SetWanderTarget(cube.Pos{14, 64, 0})
		h = tr.H()
		customer = addSurvivalPlayer(tx, mgl64.Vec3{2.5, 64, .5}).H()
		if !tr.StartTrading(customer) {
			t.Fatal("trader refused a customer")
		}
	})
	for range 100 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		tr := trader(tx, h)
		if d := horizontalDistance(tr.Position(), start); d > .5 {
			t.Fatalf("trader walked %.2f blocks while trading", d)
		}
		p, _ := customer.Entity(tx)
		tr.Hurt(1, entity.AttackDamageSource{Attacker: p})
	})
	for range 5 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		other := addSurvivalPlayer(tx, mgl64.Vec3{.5, 64, 2.5})
		if !trader(tx, h).StartTrading(other.H()) {
			t.Fatal("hurt trader kept trading with its customer")
		}
	})
}
