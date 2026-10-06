package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// GlassPane is a transparent block that can be used as a more efficient alternative to glass blocks.
type GlassPane struct {
	transparent
	clicksAndSticks
	sourceWaterDisplacer

	NorthConnection bool
	EastConnection  bool
	SouthConnection bool
	WestConnection  bool
}

// SideClosed ...
func (p GlassPane) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// BreakInfo ...
func (p GlassPane) BreakInfo() BreakInfo {
	return newBreakInfo(0.3, alwaysHarvestable, nothingEffective, silkTouchOnlyDrop(p))
}

// EncodeItem ...
func (GlassPane) EncodeItem() (name string, meta int16) {
	return "minecraft:glass_pane", meta
}

// EncodeBlock ...
func (p GlassPane) EncodeBlock() (string, map[string]any) {
	return "minecraft:glass_pane", (thinConnections{p.NorthConnection, p.EastConnection, p.SouthConnection, p.WestConnection}).properties()
}

// Model ...
func (p GlassPane) Model() world.BlockModel {
	return model.Thin{North: p.NorthConnection, East: p.EastConnection, South: p.SouthConnection, West: p.WestConnection}
}

// NeighbourUpdateTick ...
func (p GlassPane) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	updateThinBlock(pos, tx, p, thinConnections{p.NorthConnection, p.EastConnection, p.SouthConnection, p.WestConnection})
}

// UseOnBlock ...
func (p GlassPane) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	return placeThin(pos, face, p, tx, user, ctx)
}

func allGlassPane() []world.Block {
	panes := make([]world.Block, 0, 16)
	for _, connections := range allThinConnections() {
		panes = append(panes, GlassPane{
			NorthConnection: connections.north, EastConnection: connections.east,
			SouthConnection: connections.south, WestConnection: connections.west,
		})
	}
	return panes
}
