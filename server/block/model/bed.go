package model

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Bed is a model used by bed blocks.
type Bed struct{}

// BBox returns the bounding box of a bed block.
func (Bed) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	return []cube.BBox{cube.Box(0, 0, 0, 1, 9.0/16.0, 1)}
}

// FaceSolid returns whether a given face of the bed is solid.
// Only the top face of a bed is considered solid so entities may stand on it.
func (Bed) FaceSolid(_ cube.Pos, face cube.Face, _ world.BlockSource) bool {
	return face == cube.FaceUp
}
