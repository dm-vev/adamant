package model

import (
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/world"
)

// Campfire is the model used by campfires.
type Campfire struct{}

// BBox returns a flat BBox with a height of 0.4375.
func (Campfire) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	return []cube.BBox{cube.Box(0, 0, 0, 1, 0.4375, 1)}
}

// FaceSolid returns true if the face is down.
func (Campfire) FaceSolid(_ cube.Pos, face cube.Face, _ world.BlockSource) bool {
	return face == cube.FaceDown
}
