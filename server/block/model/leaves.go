package model

import (
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/world"
)

// Leaves is a model for leaves-like blocks. These blocks have a full collision box, but none of their faces
// are solid.
type Leaves struct{}

// BBox returns a physics.BBox that spans a full block.
func (Leaves) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	return fullBoxes
}

// StaticCollisionKind reports that Leaves unconditionally spans a full 1x1x1 cube.
func (Leaves) StaticCollisionKind() world.CollisionKind {
	return world.CollisionFullCube
}

// FaceSolid always returns false.
func (Leaves) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool {
	return false
}
