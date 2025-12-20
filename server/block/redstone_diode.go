package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func diodeSidePower(sidePos cube.Pos, side cube.Face, tx *world.Tx) int {
	if wire, ok := tx.Block(sidePos).(world.RedstoneWire); ok {
		return int(wire.RedstoneWirePower())
	}
	if source, ok := tx.Block(sidePos).(world.RedstonePowerSource); ok {
		return int(source.RedstoneStrongPower(side.Opposite()))
	}
	return 0
}

func diodeSideInputPower(pos cube.Pos, facing cube.Direction, tx *world.Tx) int {
	left := facing.RotateLeft().Face()
	right := facing.RotateRight().Face()
	leftPower := diodeSidePower(pos.Side(left), left, tx)
	rightPower := diodeSidePower(pos.Side(right), right, tx)
	if leftPower > rightPower {
		return leftPower
	}
	return rightPower
}
