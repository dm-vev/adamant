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
