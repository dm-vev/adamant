package block

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func redstoneTicks(ticks int) time.Duration {
	return time.Duration(max(ticks, 1)) * time.Second / 10
}

func redstonePowered(pos cube.Pos, tx *world.Tx) bool {
	return tx.RedstonePower(pos) > 0
}

// NotifyComparatorUpdate sends neighbour updates around a position and around adjacent normal blocks.
// This allows comparators reading through a solid block to update immediately.
func NotifyComparatorUpdate(pos cube.Pos, tx *world.Tx) {
	notifyComparatorUpdate(pos, tx)
}

func notifyComparatorUpdate(pos cube.Pos, tx *world.Tx) {
	if tx == nil {
		return
	}
	tx.DoBlockUpdatesAround(pos)
	for _, face := range cube.Faces() {
		sidePos := pos.Side(face)
		if !redstoneNormalBlock(sidePos, tx) {
			continue
		}
		tx.DoBlockUpdatesAround(sidePos)
	}
}

func redstoneNormalBlock(pos cube.Pos, tx *world.Tx) bool {
	return world.RedstoneFullPowerConductor(pos, tx.Block(pos), tx)
}
