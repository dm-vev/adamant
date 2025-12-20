package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// RedstoneWire is a thin redstone component that transmits power.
type RedstoneWire struct {
	carpet
	replaceable
	transparent
	sourceWaterDisplacer

	Power int
}

// BreakInfo ...
func (r RedstoneWire) BreakInfo() BreakInfo {
	return newBreakInfo(0.1, alwaysHarvestable, nothingEffective, oneOf(r))
}

// SideClosed ...
func (RedstoneWire) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// HasLiquidDrops ...
func (RedstoneWire) HasLiquidDrops() bool {
	return true
}

// NeighbourUpdateTick ...
func (r RedstoneWire) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !tx.Block(pos.Side(cube.FaceDown)).Model().FaceSolid(pos.Side(cube.FaceDown), cube.FaceUp, tx) {
		breakBlock(r, pos, tx)
	}
}

// UseOnBlock ...
func (r RedstoneWire) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, r)
	if !used {
		return false
	}
	if !tx.Block(pos.Side(cube.FaceDown)).Model().FaceSolid(pos.Side(cube.FaceDown), cube.FaceUp, tx) {
		return false
	}
	r.Power = 0
	place(tx, pos, r, user, ctx)
	return placed(ctx)
}

// EncodeItem ...
func (r RedstoneWire) EncodeItem() (name string, meta int16) {
	return "minecraft:redstone", 0
}

// EncodeBlock ...
func (r RedstoneWire) EncodeBlock() (name string, properties map[string]any) {
	return "minecraft:redstone_wire", map[string]any{"redstone_signal": int32(r.Power)}
}

// RedstoneWirePower ...
func (r RedstoneWire) RedstoneWirePower() uint8 {
	return uint8(r.Power)
}

// WithRedstoneWirePower ...
func (r RedstoneWire) WithRedstoneWirePower(power uint8) world.Block {
	r.Power = int(power)
	return r
}

// RedstoneWirePowerTo returns the power emitted towards a face.
func (r RedstoneWire) RedstoneWirePowerTo(pos cube.Pos, face cube.Face, src world.BlockSource) uint8 {
	if r.Power == 0 {
		return 0
	}
	if face == cube.FaceUp {
		return uint8(r.Power)
	}
	if face.Axis() == cube.Y {
		return 0
	}

	connections := map[cube.Face]struct{}{}
	for _, side := range cube.HorizontalFaces() {
		if r.isPowerSourceAt(pos, side, src) {
			connections[side] = struct{}{}
		}
	}

	if len(connections) == 0 {
		return uint8(r.Power)
	}
	if _, ok := connections[face]; ok {
		if _, ok := connections[face.RotateLeft()]; ok {
			return 0
		}
		if _, ok := connections[face.RotateRight()]; ok {
			return 0
		}
		return uint8(r.Power)
	}
	return 0
}

func (r RedstoneWire) isPowerSourceAt(pos cube.Pos, side cube.Face, src world.BlockSource) bool {
	sidePos := pos.Side(side)
	block := src.Block(sidePos)
	sideNormal := isNormalBlock(sidePos, src)
	if !isNormalBlock(pos.Side(cube.FaceUp), src) && sideNormal && canConnectUpwardsTo(src.Block(sidePos.Side(cube.FaceUp))) {
		return true
	}
	if canConnectTo(block, side) {
		return true
	}
	if !sideNormal && canConnectUpwardsTo(src.Block(sidePos.Side(cube.FaceDown))) {
		return true
	}
	return false
}

func canConnectUpwardsTo(b world.Block) bool {
	if _, ok := b.(world.RedstoneWire); ok {
		return true
	}
	return false
}

func canConnectTo(b world.Block, side cube.Face) bool {
	if _, ok := b.(world.RedstoneWire); ok {
		return true
	}
	if diode, ok := b.(world.RedstoneDiode); ok {
		facing := diode.RedstoneDiodeFacing().Face()
		return facing == side || facing.Opposite() == side
	}
	if obs, ok := b.(Observer); ok {
		return side == obs.Facing
	}
	if _, ok := b.(world.RedstonePowerSource); ok {
		return true
	}
	return false
}

func isNormalBlock(pos cube.Pos, src world.BlockSource) bool {
	b := src.Block(pos)
	if _, ok := b.(world.RedstonePowerSource); ok {
		return false
	}
	for _, face := range cube.Faces() {
		if !b.Model().FaceSolid(pos, face, src) {
			return false
		}
	}
	return true
}

func allRedstoneWires() (wires []world.Block) {
	for i := 0; i <= 15; i++ {
		wires = append(wires, RedstoneWire{Power: i})
	}
	return
}
