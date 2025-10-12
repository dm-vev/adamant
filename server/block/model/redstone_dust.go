package model

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

var redstoneDustBox = cube.Box(0, 0, 0, 1, 1.0/16.0, 1)

// RedstoneDust is a flat block model occupying a thin layer on the ground.
type RedstoneDust struct{}

// BBox returns the bounding box for the dust.
func (RedstoneDust) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	return []cube.BBox{redstoneDustBox}
}

// FaceSolid reports no solid faces.
func (RedstoneDust) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool {
	return false
}
