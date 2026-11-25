package hud_test

import (
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/sdk/hud"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/bossbar"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
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
		holo.Close(tx)
		if len(holo.Handles()) != 0 {
			t.Fatalf("expected 0 handles after Close, got %d", len(holo.Handles()))
		}
	})
}
