package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func diodeSidePower(sidePos cube.Pos, side cube.Face, tx *world.Tx) int {
	return tx.RedstoneDirectPowerFrom(sidePos.Side(side.Opposite()), side)
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
