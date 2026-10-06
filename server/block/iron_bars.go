package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// IronBars are blocks that serve a similar purpose to glass panes, but made of iron instead of glass.
type IronBars struct {
	transparent
	sourceWaterDisplacer

	NorthConnection bool
	EastConnection  bool
	SouthConnection bool
	WestConnection  bool
}

// BreakInfo ...
func (i IronBars) BreakInfo() BreakInfo {
	return newBreakInfo(5, pickaxeHarvestable, pickaxeEffective, oneOf(i)).withBlastResistance(6)
}

// SideClosed ...
func (i IronBars) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// EncodeItem ...
func (IronBars) EncodeItem() (name string, meta int16) {
	return "minecraft:iron_bars", 0
}

// EncodeBlock ...
func (i IronBars) EncodeBlock() (string, map[string]any) {
	return "minecraft:iron_bars", (thinConnections{i.NorthConnection, i.EastConnection, i.SouthConnection, i.WestConnection}).properties()
}

// Model ...
func (i IronBars) Model() world.BlockModel {
	return model.Thin{North: i.NorthConnection, East: i.EastConnection, South: i.SouthConnection, West: i.WestConnection}
}

// NeighbourUpdateTick ...
func (i IronBars) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	updateThinBlock(pos, tx, i, thinConnections{i.NorthConnection, i.EastConnection, i.SouthConnection, i.WestConnection})
}

// UseOnBlock ...
func (i IronBars) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	return placeThin(pos, face, i, tx, user, ctx)
}

func allIronBars() (bars []world.Block) {
	for _, connections := range allThinConnections() {
		bars = append(bars, IronBars{
			NorthConnection: connections.north, EastConnection: connections.east,
			SouthConnection: connections.south, WestConnection: connections.west,
		})
	}
	return
}
