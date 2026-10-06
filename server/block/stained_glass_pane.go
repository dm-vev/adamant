package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// StainedGlassPane is a transparent block that can be used as a more efficient alternative to glass blocks.
type StainedGlassPane struct {
	transparent
	clicksAndSticks
	sourceWaterDisplacer

	// Colour specifies the colour of the block.
	Colour item.Colour

	NorthConnection bool
	EastConnection  bool
	SouthConnection bool
	WestConnection  bool
}

// SideClosed ...
func (p StainedGlassPane) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// BreakInfo ...
func (p StainedGlassPane) BreakInfo() BreakInfo {
	return newBreakInfo(0.3, alwaysHarvestable, nothingEffective, silkTouchOnlyDrop(p))
}

// EncodeItem ...
func (p StainedGlassPane) EncodeItem() (name string, meta int16) {
	return "minecraft:" + p.Colour.String() + "_stained_glass_pane", 0
}

// EncodeBlock ...
func (p StainedGlassPane) EncodeBlock() (name string, properties map[string]any) {
	return "minecraft:" + p.Colour.String() + "_stained_glass_pane", (thinConnections{p.NorthConnection, p.EastConnection, p.SouthConnection, p.WestConnection}).properties()
}

// Model ...
func (p StainedGlassPane) Model() world.BlockModel {
	return model.Thin{North: p.NorthConnection, East: p.EastConnection, South: p.SouthConnection, West: p.WestConnection}
}

// NeighbourUpdateTick ...
func (p StainedGlassPane) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	updateThinBlock(pos, tx, p, thinConnections{p.NorthConnection, p.EastConnection, p.SouthConnection, p.WestConnection})
}

// UseOnBlock ...
func (p StainedGlassPane) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	return placeThin(pos, face, p, tx, user, ctx)
}

// allStainedGlassPane returns all connection states for stained-glass panes.
func allStainedGlassPane() (panes []world.Block) {
	for _, colour := range item.Colours() {
		for _, connections := range allThinConnections() {
			panes = append(panes, StainedGlassPane{
				Colour: colour, NorthConnection: connections.north, EastConnection: connections.east,
				SouthConnection: connections.south, WestConnection: connections.west,
			})
		}
	}
	return
}
