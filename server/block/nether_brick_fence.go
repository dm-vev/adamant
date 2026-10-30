package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// NetherBrickFence is the nether brick variant of the fence block.
type NetherBrickFence struct {
	transparent
	sourceWaterDisplacer
	NorthConnection bool
	EastConnection  bool
	SouthConnection bool
	WestConnection  bool
}

// ObstructsRain follows the native precipitation component despite open faces.
func (NetherBrickFence) ObstructsRain() bool { return true }

// BreakInfo ...
func (n NetherBrickFence) BreakInfo() BreakInfo {
	return newBreakInfo(2, pickaxeHarvestable, pickaxeEffective, oneOf(n)).withBlastResistance(6)
}

// SideClosed ...
func (NetherBrickFence) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// Model ...
func (n NetherBrickFence) Model() world.BlockModel {
	return model.Fence{North: n.NorthConnection, East: n.EastConnection, South: n.SouthConnection, West: n.WestConnection}
}

// NeighbourUpdateTick ...
func (n NetherBrickFence) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	updateFenceBlock(pos, tx, n, fenceConnections{n.NorthConnection, n.EastConnection, n.SouthConnection, n.WestConnection})
}

// UseOnBlock ...
func (n NetherBrickFence) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	return placeFence(pos, face, n, tx, user, ctx)
}

// EncodeItem ...
func (NetherBrickFence) EncodeItem() (name string, meta int16) {
	return "minecraft:nether_brick_fence", 0
}

func (n NetherBrickFence) EncodeBlock() (string, map[string]any) {
	return "minecraft:nether_brick_fence", (fenceConnections{n.NorthConnection, n.EastConnection, n.SouthConnection, n.WestConnection}).properties()
}

func allNetherBrickFence() (fences []world.Block) {
	for _, c := range allFenceConnections() {
		fences = append(fences, NetherBrickFence{
			NorthConnection: c.north, EastConnection: c.east,
			SouthConnection: c.south, WestConnection: c.west,
		})
	}
	return
}
