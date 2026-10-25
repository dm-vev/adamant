package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func diodeSidePower(sidePos cube.Pos, side cube.Face, tx *world.Tx) int {
	return tx.RedstoneDirectPowerFrom(sidePos.Side(side.Opposite()), side)
}

func diodeSideInputPower(pos cube.Pos, facing cube.Direction, tx *world.Tx) int {
	// ComparatorCapacitor::addSource (0x66d8420) only admits transporters,
	// repeaters and comparators on the sides. A direct producer such as a
	// lever, torch or redstone block is not a comparator side input.
	power := 0
	for _, face := range [...]cube.Face{facing.RotateLeft().Face(), facing.RotateRight().Face()} {
		sidePos := pos.Side(face)
		b, loaded := tx.BlockLoaded(sidePos)
		if !loaded {
			continue
		}
		switch b.(type) {
		case RedstoneWire:
			// Native wire sources are flattened into the capacitor's source
			// list. Read the current cached graph, not the wire permutation
			// that will be published after this circuit evaluation.
			power = max(power, tx.RedstonePowerFrom(pos, face))
		case RedstoneRepeater, RedstoneComparator:
			power = max(power, diodeSidePower(sidePos, face, tx))
		}
	}
	return power
}
