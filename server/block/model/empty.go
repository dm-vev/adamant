package model

import (
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/world"
)

// Empty is a model that is completely empty. It has no collision boxes or solid faces.
type Empty struct{}

// BBox returns an empty slice.
func (Empty) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	return nil
}

// StaticCollisionKind reports that Empty unconditionally has no collision boxes.
func (Empty) StaticCollisionKind() world.CollisionKind {
	return world.CollisionEmpty
}

// FaceSolid always returns false.
func (Empty) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool {
	return false
}
