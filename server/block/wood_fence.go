package block

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// FenceConnections stores the four persisted cardinal fence connections.
type fenceConnections struct {
	north, east, south, west bool
}

func allFenceConnections() []fenceConnections {
	connections := make([]fenceConnections, 0, 16)
	for mask := range 16 {
		connections = append(connections, fenceConnections{mask&1 != 0, mask&2 != 0, mask&4 != 0, mask&8 != 0})
	}
	return connections
}

func (c fenceConnections) properties() map[string]any {
	return map[string]any{
		"minecraft:connection_north": c.north,
		"minecraft:connection_east":  c.east,
		"minecraft:connection_south": c.south,
		"minecraft:connection_west":  c.west,
	}
}

func (c fenceConnections) update(pos cube.Pos, tx *world.Tx, wood bool) fenceConnections {
	return fenceConnections{
		north: fenceConnects(tx, pos, cube.FaceNorth, wood),
		east:  fenceConnects(tx, pos, cube.FaceEast, wood),
		south: fenceConnects(tx, pos, cube.FaceSouth, wood),
		west:  fenceConnects(tx, pos, cube.FaceWest, wood),
	}
}

func fenceConnectionsOf(block world.Block) fenceConnections {
	switch fence := block.(type) {
	case WoodFence:
		return fenceConnections{fence.NorthConnection, fence.EastConnection, fence.SouthConnection, fence.WestConnection}
	case NetherBrickFence:
		return fenceConnections{fence.NorthConnection, fence.EastConnection, fence.SouthConnection, fence.WestConnection}
	default:
		panic("unsupported fence")
	}
}

func withFenceConnections(block world.Block, c fenceConnections) world.Block {
	switch fence := block.(type) {
	case WoodFence:
		fence.NorthConnection, fence.EastConnection, fence.SouthConnection, fence.WestConnection = c.north, c.east, c.south, c.west
		return fence
	case NetherBrickFence:
		fence.NorthConnection, fence.EastConnection, fence.SouthConnection, fence.WestConnection = c.north, c.east, c.south, c.west
		return fence
	default:
		panic("unsupported fence")
	}
}

func placeFence(pos cube.Pos, face cube.Face, block world.Block, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, block)
	if !used {
		return false
	}
	connections := fenceConnectionsOf(block).update(pos, tx, isWoodFence(block))
	place(tx, pos, withFenceConnections(block, connections), user, ctx)
	return placed(ctx)
}

func updateFenceBlock(pos cube.Pos, tx *world.Tx, block world.Block, c fenceConnections) {
	updated := c.update(pos, tx, isWoodFence(block))
	if updated != c {
		tx.SetBlock(pos, withFenceConnections(block, updated), nil)
	}
}

func isWoodFence(block world.Block) bool {
	_, ok := block.(WoodFence)
	return ok
}

func reconstructFenceConnections(block world.Block, pos cube.Pos, missing []string, bs world.BlockSource) world.Block {
	connections := fenceConnectionsOf(block)
	changed := false
	wood := isWoodFence(block)
	if hasMissingProperty(missing, "minecraft:connection_north") {
		connections.north = fenceConnects(bs, pos, cube.FaceNorth, wood)
		changed = true
	}
	if hasMissingProperty(missing, "minecraft:connection_east") {
		connections.east = fenceConnects(bs, pos, cube.FaceEast, wood)
		changed = true
	}
	if hasMissingProperty(missing, "minecraft:connection_south") {
		connections.south = fenceConnects(bs, pos, cube.FaceSouth, wood)
		changed = true
	}
	if hasMissingProperty(missing, "minecraft:connection_west") {
		connections.west = fenceConnects(bs, pos, cube.FaceWest, wood)
		changed = true
	}
	if !changed {
		return block
	}
	return withFenceConnections(block, connections)
}

func fenceConnects(bs world.BlockSource, pos cube.Pos, face cube.Face, wood bool) bool {
	sidePos := pos.Side(face)
	sideBlock := bs.Block(sidePos)
	switch sideBlock.(type) {
	case WoodFence:
		if wood {
			return true
		}
	case NetherBrickFence:
		if !wood {
			return true
		}
	}
	if sideBlock.Model().FaceSolid(sidePos, face.Opposite(), bs) {
		return true
	}
	_, ok := sideBlock.Model().(model.FenceGate)
	return ok
}

func (w WoodFence) ReconstructMissingState(pos cube.Pos, missing []string, bs world.BlockSource) world.Block {
	return reconstructFenceConnections(w, pos, missing, bs)
}

func (n NetherBrickFence) ReconstructMissingState(pos cube.Pos, missing []string, bs world.BlockSource) world.Block {
	return reconstructFenceConnections(n, pos, missing, bs)
}

// WoodFence are blocks similar to Walls, which cannot normally be jumped over. Unlike walls however,
// they allow the player (but not mobs) to see through them, making for excellent barriers.
type WoodFence struct {
	transparent
	bass
	sourceWaterDisplacer

	// Wood is the type of wood of the fence. This field must have one of the values found in the wood
	// package.
	Wood            WoodType
	NorthConnection bool
	EastConnection  bool
	SouthConnection bool
	WestConnection  bool
}

// BreakInfo ...
func (w WoodFence) BreakInfo() BreakInfo {
	return newBreakInfo(2, alwaysHarvestable, axeEffective, oneOf(w)).withBlastResistance(3)
}

// SideClosed ...
func (WoodFence) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// FlammabilityInfo ...
func (w WoodFence) FlammabilityInfo() FlammabilityInfo {
	if !w.Wood.Flammable() {
		return newFlammabilityInfo(0, 0, false)
	}
	return newFlammabilityInfo(5, 20, true)
}

// FuelInfo ...
func (w WoodFence) FuelInfo() item.FuelInfo {
	if !w.Wood.Flammable() {
		return item.FuelInfo{}
	}
	return newFuelInfo(time.Second * 15)
}

// EncodeBlock ...
func (w WoodFence) EncodeBlock() (name string, properties map[string]any) {
	return "minecraft:" + w.Wood.String() + "_fence", (fenceConnections{w.NorthConnection, w.EastConnection, w.SouthConnection, w.WestConnection}).properties()
}

// Model ...
func (w WoodFence) Model() world.BlockModel {
	return model.Fence{Wood: true, North: w.NorthConnection, East: w.EastConnection, South: w.SouthConnection, West: w.WestConnection}
}

// NeighbourUpdateTick ...
func (w WoodFence) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	updateFenceBlock(pos, tx, w, fenceConnections{w.NorthConnection, w.EastConnection, w.SouthConnection, w.WestConnection})
}

// UseOnBlock ...
func (w WoodFence) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	return placeFence(pos, face, w, tx, user, ctx)
}

// EncodeItem ...
func (w WoodFence) EncodeItem() (name string, meta int16) {
	return "minecraft:" + w.Wood.String() + "_fence", 0
}

// allFence ...
func allFence() (fence []world.Block) {
	for _, w := range WoodTypes() {
		for _, c := range allFenceConnections() {
			fence = append(fence, WoodFence{
				Wood: w, NorthConnection: c.north, EastConnection: c.east,
				SouthConnection: c.south, WestConnection: c.west,
			})
		}
	}
	return
}
