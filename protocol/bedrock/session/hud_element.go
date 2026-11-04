package session

import (
	"github.com/df-mc/dragonfly/server/player/hud"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// bedrockHudElement returns the ID SetHud sends for a HUD element.
func bedrockHudElement(e hud.Element) int32 {
	switch e {
	case hud.PaperDoll():
		return mc.HudElementPaperDoll
	case hud.Armour():
		return mc.HudElementArmour
	case hud.ToolTips():
		return mc.HudElementToolTips
	case hud.TouchControls():
		return mc.HudElementTouchControls
	case hud.Crosshair():
		return mc.HudElementCrosshair
	case hud.HotBar():
		return mc.HudElementHotBar
	case hud.Health():
		return mc.HudElementHealth
	case hud.ProgressBar():
		return mc.HudElementProgressBar
	case hud.Hunger():
		return mc.HudElementHunger
	case hud.AirBubbles():
		return mc.HudElementAirBubbles
	case hud.HorseHealth():
		return mc.HudElementHorseHealth
	case hud.StatusEffects():
		return mc.HudElementStatusEffects
	case hud.ItemText():
		return mc.HudElementItemText
	}
	return mc.HudElementPaperDoll
}
