package sdktest_test

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dm-vev/adamant/sdk/event"
	"github.com/dm-vev/adamant/sdk/plugin"
	"github.com/dm-vev/adamant/sdk/schedule"
	"github.com/dm-vev/adamant/sdk/sdktest"
	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/entity"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestPluginUseOrderedReloadAndAsyncSchedule(t *testing.T) {
	app := sdktest.NewApp(t)
	var order []string
	var reloads atomic.Int32

	pApp := plugin.NewFunc("shop", func(*plugin.Context) {
		order = append(order, "shop")
	}, plugin.WithDepends("perm", "db"), plugin.WithOnReload(func(*plugin.Context) error {
		reloads.Add(1)
		return nil
	}))
	pPerm := plugin.NewFunc("perm", func(*plugin.Context) {
		order = append(order, "perm")
	}, plugin.WithDepends("db"))
	pDB := plugin.NewFunc("db", func(*plugin.Context) {
		order = append(order, "db")
	})

	if err := app.UseOrdered(pApp, pPerm, pDB); err != nil {
		t.Fatalf("UseOrdered: %v", err)
	}
	if !slices.Equal(order, []string{"db", "perm", "shop"}) {
		t.Fatalf("UseOrdered order = %v, want [db perm shop]", order)
	}

	if errs := app.Reload(); len(errs) != 0 || reloads.Load() != 1 {
		t.Fatalf("Reload = (%v, count=%d)", errs, reloads.Load())
	}
	if err := app.ReloadPlugin("shop"); err != nil || reloads.Load() != 2 {
		t.Fatalf("ReloadPlugin = (%v, count=%d)", err, reloads.Load())
	}

	// Missing dependency error.
	if err := app.UseOrdered(plugin.NewFunc("orphan", nil, plugin.WithDepends("ghost"))); err == nil {
		t.Fatal("expected error for missing dependency")
	}
	// Cycle detection error.
	c1 := plugin.NewFunc("c1", nil, plugin.WithDepends("c2"))
	c2 := plugin.NewFunc("c2", nil, plugin.WithDepends("c1"))
	if err := app.UseOrdered(c1, c2); err == nil {
		t.Fatal("expected error for dependency cycle")
	}

	// Test schedule.Async + ThenWorld delivery.
	w := sdktest.NewWorld(t)
	doneCh := make(chan int, 1)
	app.Use(plugin.Func("worker", func(ctx *plugin.Context) {
		schedule.Async(ctx, func(runCtx context.Context) (int, error) {
			if runCtx.Err() != nil {
				return 0, runCtx.Err()
			}
			return 42, nil
		}).ThenWorld(w, func(tx *world.Tx, val int, err error) {
			if err != nil || tx == nil {
				return
			}
			doneCh <- val
		})
	}, nil))

	select {
	case v := <-doneCh:
		if v != 42 {
			t.Fatalf("Async ThenWorld val = %d, want 42", v)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Async.ThenWorld")
	}

	// Verify context cancellation on plugin disable.
	cancelledCh := make(chan error, 1)
	app2 := sdktest.NewApp(t)
	app2.Use(plugin.Func("cancel-worker", func(ctx *plugin.Context) {
		schedule.Async(ctx, func(runCtx context.Context) (int, error) {
			<-runCtx.Done()
			cancelledCh <- runCtx.Err()
			return 0, runCtx.Err()
		})
	}, nil))
	app2.Disable()
	select {
	case err := <-cancelledCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Async context cancel on Disable")
	}
}

func TestExtendedPlayerAndWorldEvents(t *testing.T) {
	w := sdktest.NewWorld(t)
	fp := sdktest.SpawnPlayer(t, w, "Alice", mgl64.Vec3{0.5, 64, 0.5})

	var (
		xpHits, slotHits, pickupHits, healHits, punchHits atomic.Int32
		explosionHits, weatherHits                        atomic.Int32
	)

	event.OnWorld[player.ExperienceGainEvent](nil, w, event.Normal, func(ev *player.ExperienceGainEvent) {
		xpHits.Add(1)
		ev.Amount *= 2
	})
	event.OnWorld[player.HeldSlotChangeEvent](nil, w, event.Normal, func(ev *player.HeldSlotChangeEvent) {
		slotHits.Add(1)
		if ev.To == 3 {
			ev.Cancel()
		}
	})
	event.OnWorld[player.ItemPickupEvent](nil, w, event.Normal, func(ev *player.ItemPickupEvent) {
		pickupHits.Add(1)
		ev.Cancel()
	})
	event.OnWorld[player.HealEvent](nil, w, event.Normal, func(ev *player.HealEvent) {
		healHits.Add(1)
		ev.Health = 4
	})
	event.OnWorld[player.PunchAirEvent](nil, w, event.Normal, func(ev *player.PunchAirEvent) {
		punchHits.Add(1)
	})
	event.OnWorld[world.ExplosionEvent](nil, w, event.Normal, func(ev *world.ExplosionEvent) {
		explosionHits.Add(1)
		ev.Cancel()
	})
	event.OnWorld[world.WeatherChangeEvent](nil, w, event.Normal, func(ev *world.WeatherChangeEvent) {
		weatherHits.Add(1)
		ev.Cancel()
	})

	fp.Run(func(p *player.Player, tx *world.Tx) {
		if got := p.AddExperience(10); got != 20 {
			t.Fatalf("AddExperience = %d, want 20 (doubled by event)", got)
		}
		if err := p.SetHeldSlot(3); err != nil {
			t.Fatalf("SetHeldSlot: %v", err)
		}
		if _, ok := p.Collect(item.NewStack(item.Diamond{}, 1)); ok {
			t.Fatal("expected ItemPickupEvent to cancel Collect")
		}
		p.Hurt(10, entity.VoidDamageSource{})
		if healed := p.Heal(1, entity.FoodHealingSource{}); healed != 4 {
			t.Fatalf("Heal = %v, want 4 (overridden by HealEvent)", healed)
		}
		p.PunchAir()

		// Place stone and trigger a cancelled explosion -> stone must remain.
		pos := cube.Pos{2, 64, 2}
		tx.SetBlock(pos, block.Stone{}, nil)
		block.ExplosionConfig{}.Explode(tx, world.BlockExplosionSource{Pos: pos, Block: block.TNT{}, ExplosionSize: 4})
		if _, ok := tx.Block(pos).(block.Stone); !ok {
			t.Fatalf("expected Stone preserved by cancelled ExplosionEvent, got %T", tx.Block(pos))
		}
	})

	w.StartRaining(time.Minute)

	if xpHits.Load() != 1 || slotHits.Load() != 1 || pickupHits.Load() != 1 || healHits.Load() != 1 || punchHits.Load() != 1 {
		t.Fatalf("player event counts: xp=%d slot=%d pickup=%d heal=%d punch=%d",
			xpHits.Load(), slotHits.Load(), pickupHits.Load(), healHits.Load(), punchHits.Load())
	}
	if explosionHits.Load() != 1 || weatherHits.Load() != 1 {
		t.Fatalf("world event counts: explosion=%d weather=%d", explosionHits.Load(), weatherHits.Load())
	}
}
