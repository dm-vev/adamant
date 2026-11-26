package model

import (
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/world"
)

// EndRod is a model used by end rod blocks.
type EndRod struct {
	// Axis is the axis which the end rod faces.
	Axis cube.Axis
}

// BBox ...
func (e EndRod) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	return []cube.BBox{cube.Box(0.375, 0.375, 0.375, 0.625, 0.625, 0.625).Stretch(e.Axis, 0.375)}
}

// FaceSolid ...
func (EndRod) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool {
	return false
}
