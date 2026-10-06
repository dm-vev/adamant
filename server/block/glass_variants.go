package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

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
	clicksAndSticks
	sourceWaterDisplacer

	NorthConnection bool
	EastConnection  bool
	SouthConnection bool
	WestConnection  bool
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
func (p HardenedGlassPane) EncodeBlock() (string, map[string]any) {
	return "minecraft:hard_glass_pane", (thinConnections{p.NorthConnection, p.EastConnection, p.SouthConnection, p.WestConnection}).properties()
}

// Model ...
func (p HardenedGlassPane) Model() world.BlockModel {
	return model.Thin{North: p.NorthConnection, East: p.EastConnection, South: p.SouthConnection, West: p.WestConnection}
}

// NeighbourUpdateTick ...
func (p HardenedGlassPane) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	updateThinBlock(pos, tx, p, thinConnections{p.NorthConnection, p.EastConnection, p.SouthConnection, p.WestConnection})
}

// UseOnBlock ...
func (p HardenedGlassPane) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	return placeThin(pos, face, p, tx, user, ctx)
}

func allHardenedGlassPane() (panes []world.Block) {
	for _, connections := range allThinConnections() {
		panes = append(panes, HardenedGlassPane{
			NorthConnection: connections.north, EastConnection: connections.east,
			SouthConnection: connections.south, WestConnection: connections.west,
		})
	}
	return
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
	clicksAndSticks
	sourceWaterDisplacer

	Colour item.Colour

	NorthConnection bool
	EastConnection  bool
	SouthConnection bool
	WestConnection  bool
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
	return "minecraft:hard_" + p.Colour.String() + "_stained_glass_pane", (thinConnections{p.NorthConnection, p.EastConnection, p.SouthConnection, p.WestConnection}).properties()
}

// Model ...
func (p HardenedStainedGlassPane) Model() world.BlockModel {
	return model.Thin{North: p.NorthConnection, East: p.EastConnection, South: p.SouthConnection, West: p.WestConnection}
}

// NeighbourUpdateTick ...
func (p HardenedStainedGlassPane) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	updateThinBlock(pos, tx, p, thinConnections{p.NorthConnection, p.EastConnection, p.SouthConnection, p.WestConnection})
}

// UseOnBlock ...
func (p HardenedStainedGlassPane) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	return placeThin(pos, face, p, tx, user, ctx)
}

// allHardenedStainedGlass returns all hardened stained glass block states.
func allHardenedStainedGlass() (blocks []world.Block) {
	for _, c := range item.Colours() {
		blocks = append(blocks, HardenedStainedGlass{Colour: c})
	}
	return
}

// allHardenedStainedGlassPanes returns all hardened stained glass pane block states.
func allHardenedStainedGlassPanes() (blocks []world.Block) {
	for _, c := range item.Colours() {
		for _, connections := range allThinConnections() {
			blocks = append(blocks, HardenedStainedGlassPane{
				Colour: c, NorthConnection: connections.north, EastConnection: connections.east,
				SouthConnection: connections.south, WestConnection: connections.west,
			})
		}
	}
	return
}
