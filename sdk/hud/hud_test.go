package hud_test

import (
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/dm-vev/adamant/sdk/hud"
	"github.com/dm-vev/adamant/server/client/clienttest"
	"github.com/dm-vev/adamant/server/entity"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/player/bossbar"
	"github.com/dm-vev/adamant/server/player/skin"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

func TestHUDBuildersAndHologramLifecycle(t *testing.T) {
	sb := hud.NewScoreboard("Arena", "Line 1").Line("Line 2").Descending().NoPadding().Build()
	if sb.Name().String() != "Arena" || len(sb.Lines()) != 2 {
		t.Fatalf("Scoreboard = %q lines=%d", sb.Name().String(), len(sb.Lines()))
	}

	// Explicit 0 health vs default 1.0 health without sentinel booleans.
	barDefault := hud.NewBossBar("Boss").Build()
	if barDefault.HealthPercentage() != 1.0 {
		t.Fatalf("default bossbar health = %v, want 1.0", barDefault.HealthPercentage())
	}
	barZero := hud.NewBossBar("Empty").ID("boss_1").Health(0).Colour(bossbar.Red()).Build()
	if barZero.HealthPercentage() != 0 || barZero.ID() != "boss_1" || barZero.Colour() != bossbar.Red() {
		t.Fatalf("explicit bossbar mismatch: id=%q hp=%v col=%v", barZero.ID(), barZero.HealthPercentage(), barZero.Colour())
	}

	ti := hud.NewTitle("Victory").
		Subtitle("Round 1").
		ActionText("+100 XP").
		FadeIn(100 * time.Millisecond).
		Duration(time.Second).
		FadeOut(200 * time.Millisecond).
		Build()
	if ti.Text().String() != "Victory" || ti.Subtitle().String() != "Round 1" || ti.ActionText().String() != "+100 XP" {
		t.Fatalf("Title mismatch: %+v", ti)
	}

	var picked string
	dlg := hud.NewDialogue("Merchant").
		Body("Buy something?").
		Button("Trade", func(*player.Player, *world.Tx) { picked = "trade" }).
		Build()
	if dlg.Title() != "Merchant" || len(dlg.Buttons()) != 1 {
		t.Fatalf("Dialogue mismatch: title=%q buttons=%d", dlg.Title(), len(dlg.Buttons()))
	}
	_ = picked

	// Test Hologram spawn, update, move, and close in a synchronous world.
	world.DefaultBlockRegistry.Finalize()
	w := world.Config{
		Log:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		Synchronous:     true,
		RandomTickSpeed: -1,
		Generator:       world.NopGenerator{},
		Provider:        world.NopProvider{},
		Blocks:          world.DefaultBlockRegistry,
		Entities:        entity.DefaultRegistry,
	}.New()
	t.Cleanup(func() { _ = w.Close() })

	<-w.Exec(func(tx *world.Tx) {
		holo := hud.NewHologram(mgl64.Vec3{0.5, 65, 0.5}, "Top", "Bottom").Spacing(0.3).Spawn(tx)
		if !slices.Equal(holo.Lines(), []string{"Top", "Bottom"}) || len(holo.Handles()) != 2 {
			t.Fatalf("spawned hologram lines=%v handles=%d", holo.Lines(), len(holo.Handles()))
		}
		holo.SetLines(tx, "Only One")
		if len(holo.Handles()) != 1 || !slices.Equal(holo.Lines(), []string{"Only One"}) {
			t.Fatalf("updated hologram lines=%v handles=%d", holo.Lines(), len(holo.Handles()))
		}
		holo.Move(tx, mgl64.Vec3{5, 70, 5})
		if holo.Position() != (mgl64.Vec3{5, 70, 5}) {
			t.Fatalf("moved pos = %v", holo.Position())
		}

		// Test HUD elements, input locks, nametag/scoretag, and per-player visibility.
		ph := world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 65, 0.5}}.New(player.Type, player.Config{
			Client: clienttest.New(),
			UUID:   uuid.New(),
			Name:   "Viewer",
			Skin:   skin.New(64, 64),
		})
		viewer := tx.AddEntity(ph).(*player.Player)

		hud.HideHUD(viewer, hud.HotBar(), hud.Health())
		if !hud.HUDHidden(viewer, hud.HotBar()) || !hud.HUDHidden(viewer, hud.Health()) {
			t.Fatal("expected HotBar and Health hidden")
		}
		hud.ResetHUD(viewer)
		if hud.HUDHidden(viewer, hud.HotBar()) {
			t.Fatal("expected HotBar restored after ResetHUD")
		}

		hud.LockInput(viewer, hud.LockMovement(), hud.LockCamera())
		if !hud.InputLocked(viewer, hud.LockMovement()) || !hud.InputLocked(viewer, hud.LockCamera()) {
			t.Fatal("expected Movement and Camera input locks active")
		}
		hud.ClearInputLocks(viewer)
		if hud.InputLocked(viewer, hud.LockMovement()) {
			t.Fatal("expected input locks cleared")
		}

		hud.SetNameTag(viewer, "Hero")
		hud.SetScoreTag(viewer, "20 HP")
		holo.HideFrom(viewer, tx)
		holo.ShowTo(viewer, tx)
		hud.Vanish(viewer, viewer)
		hud.Unvanish(viewer, viewer)

		holo.Close(tx)
		if len(holo.Handles()) != 0 {
			t.Fatalf("expected 0 handles after Close, got %d", len(holo.Handles()))
		}
	})
}
