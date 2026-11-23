package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func trySpawnConstructedGolem(tx *world.Tx, pos cube.Pos) bool {
	return trySpawnGolemPattern(tx, pos) || trySpawnCopperGolemPattern(tx, pos)
}
