package session

import (
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/player/bossbar"
	"github.com/df-mc/dragonfly/server/player/hud"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// TestHudElementValuesMatchProtocol holds every HUD element of hud.All, and
// the zero Element, to the ID SetHud sent for it when Element.Uint8 returned
// the protocol's ID.
func TestHudElementValuesMatchProtocol(t *testing.T) {
	want := []int64{
		v2193.HudElementPaperDoll, v2193.HudElementArmour, v2193.HudElementToolTips, v2193.HudElementTouchControls,
		v2193.HudElementCrosshair, v2193.HudElementHotBar, v2193.HudElementHealth, v2193.HudElementProgressBar,
		v2193.HudElementHunger, v2193.HudElementAirBubbles, v2193.HudElementHorseHealth, v2193.HudElementStatusEffects,
		v2193.HudElementItemText,
	}
	all := hud.All()
	if len(all) != len(want) {
		t.Fatalf("hud.All() has %d elements, want %d", len(all), len(want))
	}
	values := make([]gameValue, 0, len(all)+1)
	for i, e := range all {
		values = append(values, gameValue{name: fmt.Sprintf("hud.All()[%d]", i), game: int64(bedrockHudElement(e)), want: want[i]})
	}
	values = append(values, gameValue{"hud.Element{}", int64(bedrockHudElement(hud.Element{})), v2193.HudElementPaperDoll})
	checkGameValues(t, values)
}

// TestBossBarColourValues holds every boss bar colour, and the zero Colour,
// to the colour BossEvent sent for it when Colour.Uint8 returned Bedrock's
// number: 0 to 7 in the order of the constructors.
func TestBossBarColourValues(t *testing.T) {
	checkGameValues(t, []gameValue{
		{"Pink", int64(bedrockBossBarColour(bossbar.Pink())), 0},
		{"Blue", int64(bedrockBossBarColour(bossbar.Blue())), 1},
		{"Red", int64(bedrockBossBarColour(bossbar.Red())), 2},
		{"Green", int64(bedrockBossBarColour(bossbar.Green())), 3},
		{"Yellow", int64(bedrockBossBarColour(bossbar.Yellow())), 4},
		{"Purple", int64(bedrockBossBarColour(bossbar.Purple())), 5},
		{"RebeccaPurple", int64(bedrockBossBarColour(bossbar.RebeccaPurple())), 6},
		{"White", int64(bedrockBossBarColour(bossbar.White())), 7},
		{"Colour{}", int64(bedrockBossBarColour(bossbar.Colour{})), 0},
	})
}
