package block

import "github.com/df-mc/dragonfly/server/item"

// HardenedGlass is a hardened glass block.
type HardenedGlass struct {
	solid
	transparent
	clicksAndSticks
}

// BreakInfo ...
func (g HardenedGlass) BreakInfo() BreakInfo {
	return newBreakInfo(0.3, alwaysHarvestable, nothingEffective, silkTouchOnlyDrop(g)).withBlastResistance(1.5)
}

// EncodeItem ...
func (HardenedGlass) EncodeItem() (name string, meta int16) {
	return "minecraft:hard_glass", 0
}

// EncodeBlock ...
func (HardenedGlass) EncodeBlock() (string, map[string]any) {
	return "minecraft:hard_glass", nil
}

// HardenedGlassPane is a hardened glass pane.
type HardenedGlassPane struct {
	transparent
	thin
	clicksAndSticks
	sourceWaterDisplacer
}

// BreakInfo ...
func (p HardenedGlassPane) BreakInfo() BreakInfo {
	return newBreakInfo(0.3, alwaysHarvestable, nothingEffective, silkTouchOnlyDrop(p)).withBlastResistance(1.5)
}

// EncodeItem ...
func (HardenedGlassPane) EncodeItem() (name string, meta int16) {
	return "minecraft:hard_glass_pane", 0
}

// EncodeBlock ...
func (HardenedGlassPane) EncodeBlock() (string, map[string]any) {
	return "minecraft:hard_glass_pane", nil
}

// HardenedStainedGlass is a hardened stained glass block.
type HardenedStainedGlass struct {
	transparent
	solid
	clicksAndSticks

	Colour item.Colour
}

// BreakInfo ...
func (g HardenedStainedGlass) BreakInfo() BreakInfo {
	return newBreakInfo(0.3, alwaysHarvestable, nothingEffective, silkTouchOnlyDrop(g)).withBlastResistance(1.5)
}

// EncodeItem ...
func (g HardenedStainedGlass) EncodeItem() (name string, meta int16) {
	return "minecraft:hard_" + g.Colour.String() + "_stained_glass", 0
}

// EncodeBlock ...
func (g HardenedStainedGlass) EncodeBlock() (string, map[string]any) {
	return "minecraft:hard_" + g.Colour.String() + "_stained_glass", nil
}

// HardenedStainedGlassPane is a hardened stained glass pane.
type HardenedStainedGlassPane struct {
	transparent
	thin
	clicksAndSticks
	sourceWaterDisplacer

	Colour item.Colour
}

// BreakInfo ...
func (p HardenedStainedGlassPane) BreakInfo() BreakInfo {
	return newBreakInfo(0.3, alwaysHarvestable, nothingEffective, silkTouchOnlyDrop(p)).withBlastResistance(1.5)
}

// EncodeItem ...
func (p HardenedStainedGlassPane) EncodeItem() (name string, meta int16) {
	return "minecraft:hard_" + p.Colour.String() + "_stained_glass_pane", 0
}

// EncodeBlock ...
func (p HardenedStainedGlassPane) EncodeBlock() (string, map[string]any) {
	return "minecraft:hard_" + p.Colour.String() + "_stained_glass_pane", nil
}

// TintedGlass is a glass block that blocks light.
type TintedGlass struct {
	solid
	transparent
	clicksAndSticks
}

// LightDiffusionLevel ...
func (TintedGlass) LightDiffusionLevel() uint8 {
	return 15
}

// BreakInfo ...
func (g TintedGlass) BreakInfo() BreakInfo {
	return newBreakInfo(0.3, alwaysHarvestable, nothingEffective, silkTouchOnlyDrop(g))
}

// EncodeItem ...
func (TintedGlass) EncodeItem() (name string, meta int16) {
	return "minecraft:tinted_glass", 0
}

// EncodeBlock ...
func (TintedGlass) EncodeBlock() (string, map[string]any) {
	return "minecraft:tinted_glass", nil
}
