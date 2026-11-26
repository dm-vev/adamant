package entity

import (
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/world"
)

func trySpawnConstructedGolem(tx *world.Tx, pos cube.Pos) bool {
	return trySpawnGolemPattern(tx, pos) || trySpawnCopperGolemPattern(tx, pos)
}
