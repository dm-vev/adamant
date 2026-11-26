package main

import (
	"testing"
	"time"

	"github.com/dm-vev/adamant/sdk/hud"
	"github.com/dm-vev/adamant/sdk/plugin"
	"github.com/dm-vev/adamant/sdk/sdktest"
	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/item/enchantment"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestMinigameArenaLifecycle(t *testing.T) {
	lobby := sdktest.NewWorld(t)
	app := sdktest.NewApp(t)
	mg := newMinigameService()

	var hostCtx *plugin.Context
	app.Use(plugin.Func("minigame", func(ctx *plugin.Context) {
		hostCtx = ctx
		mg.enable(ctx)
	}, nil))

	if mg.arena == nil {
		t.Fatal("arena world not opened")
	}

	// Verify platform blocks were stamped into arena by worldgen.Structure.
	<-mg.arena.Exec(func(tx *world.Tx) {
		if _, ok := tx.Block(cube.Pos{0, 63, 0}).(block.Quartz); !ok {
			t.Fatalf("expected Quartz platform block, got %T", tx.Block(cube.Pos{0, 63, 0}))
		}
	})

	// Spawn player in lobby and transfer into arena via joinArena.
	fp := sdktest.SpawnPlayer(t, lobby, "Challenger", mgl64.Vec3{0.5, 64, 0.5})
	fp.Run(func(p *player.Player, tx *world.Tx) {
		if !mg.joinArena(hostCtx, p, tx) {
			t.Fatal("joinArena returned false")
		}
	})

	// Inspect transferred player kit, lock state, and Blink Wand action inside arena.
	<-mg.arena.Exec(func(tx *world.Tx) {
		ent, ok := fp.Handle().Entity(tx)
		if !ok {
			t.Fatal("player did not arrive in arena world")
		}
		ap := ent.(*player.Player)
		if !hud.InputLocked(ap, hud.LockMovement()) {
			t.Fatal("expected LockMovement during intro")
		}
		main, _ := ap.HeldItems()
		if main.CustomName() != "Blink Wand" {
			t.Fatalf("held item = %q, want Blink Wand", main.CustomName())
		}
		slots := ap.Inventory().Items()
		foundSword := false
		for _, st := range slots {
			if st.CustomName() == "Sky Blade" {
				if _, ok := st.Enchantment(enchantment.Sharpness); ok && st.Unbreakable() {
					foundSword = true
				}
			}
		}
		if !foundSword {
			t.Fatal("Sky Blade kit sword missing or missing Sharpness/Unbreakable")
		}
		before := blinkUses.Load()
		ap.UseItem()
		if blinkUses.Load() != before+1 {
			t.Fatalf("blinkUses = %d, want %d", blinkUses.Load(), before+1)
		}
	})

	// Advance 3 ticks so schedule.Every updates bossbar and schedule.After unlocks movement.
	for range 3 {
		mg.arena.AdvanceTick()
	}
	if mg.roundTicks.Load() < 3 {
		t.Fatalf("roundTicks = %d, want >= 3", mg.roundTicks.Load())
	}
	<-mg.arena.Exec(func(tx *world.Tx) {
		ent, ok := fp.Handle().Entity(tx)
		if !ok {
			t.Fatal("player missing from arena")
		}
		ap := ent.(*player.Player)
		if hud.InputLocked(ap, hud.LockMovement()) {
			t.Fatal("expected LockMovement cleared after scheduled intro delay")
		}
	})

	// Wait briefly for Async leaderboard delivery onto arena world.
	deadline := time.Now().Add(3 * time.Second)
	for mg.leaderboard.Load() == nil && time.Now().Before(deadline) {
		mg.arena.AdvanceTick()
		time.Sleep(10 * time.Millisecond)
	}
	if mg.leaderboard.Load() == nil || *mg.leaderboard.Load() != "Top Player: Alex (12 wins)" {
		t.Fatalf("leaderboard = %v", mg.leaderboard.Load())
	}

	// Disabling app must close the dynamic arena world cleanly.
	app.Disable()
	if len(mg.manager.Names()) != 0 {
		t.Fatalf("expected arena unloaded on disable, remaining: %v", mg.manager.Names())
	}
}
