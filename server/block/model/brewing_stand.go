package model

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// BrewingStand is a model used by brewing stands.
type BrewingStand struct{}

// BBox ...
func (b BrewingStand) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	return []cube.BBox{
		cube.Box(.4375, 0, .4375, .5625, .875, .5625),
		cube.Box(0, 0, 0, 1, .125, 1),
	}
}

// FaceSolid ...
func (b BrewingStand) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool {
	return false
}
