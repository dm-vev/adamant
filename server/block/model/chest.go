package model

import (
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/world"
)

// Chest is the model of a chest. It is just barely not a full block, having a slightly reduced with on all
// axes.
type Chest struct{}

// BBox returns a physics.BBox that is slightly smaller than a full block.
func (Chest) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	return []cube.BBox{cube.Box(0.025, 0, 0.025, 0.975, 0.95, 0.975)}
}

// FaceSolid always returns false.
func (Chest) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool {
	return false
}
