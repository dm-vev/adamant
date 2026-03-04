package block

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func redstoneTicks(ticks int) time.Duration {
	return time.Duration(ticks) * (time.Second / 20)
}

func redstonePowered(pos cube.Pos, tx *world.Tx) bool {
	for _, face := range cube.Faces() {
		if world.RedstonePowerFromSide(tx, pos, face) > 0 {
			return true
		}
	}
	return false
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

func redstoneNormalBlock(pos cube.Pos, src world.BlockSource) bool {
	b := src.Block(pos)
	for _, face := range cube.Faces() {
		if !b.Model().FaceSolid(pos, face, src) {
			return false
		}
	}
	return true
}
