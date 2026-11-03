package session

import (
	"github.com/df-mc/dragonfly/server/player/hud"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// bedrockHudElement returns the ID SetHud sends for a HUD element.
func bedrockHudElement(e hud.Element) int32 {
	switch e {
	case hud.PaperDoll():
		return v2193.HudElementPaperDoll
	case hud.Armour():
		return v2193.HudElementArmour
	case hud.ToolTips():
		return v2193.HudElementToolTips
	case hud.TouchControls():
		return v2193.HudElementTouchControls
	case hud.Crosshair():
		return v2193.HudElementCrosshair
	case hud.HotBar():
		return v2193.HudElementHotBar
	case hud.Health():
		return v2193.HudElementHealth
	case hud.ProgressBar():
		return v2193.HudElementProgressBar
	case hud.Hunger():
		return v2193.HudElementHunger
	case hud.AirBubbles():
		return v2193.HudElementAirBubbles
	case hud.HorseHealth():
		return v2193.HudElementHorseHealth
	case hud.StatusEffects():
		return v2193.HudElementStatusEffects
	case hud.ItemText():
		return v2193.HudElementItemText
	}
	return v2193.HudElementPaperDoll
}
