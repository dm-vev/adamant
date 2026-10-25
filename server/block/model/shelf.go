package model

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Shelf is the model of a shelf: a back panel with a lip along the top and
// bottom of its front.
type Shelf struct {
	// Facing is the direction the front of the shelf faces.
	Facing cube.Direction
}

// BBox ports the shape of ShelfBlock.
func (s Shelf) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	// The boxes of a shelf facing north, as Java defines them, in pixels.
	boxes := [][6]float64{{0, 12, 11, 16, 16, 13}, {0, 0, 13, 16, 16, 16}, {0, 0, 11, 16, 4, 13}}
	out := make([]cube.BBox, 0, len(boxes))
	for _, b := range boxes {
		minX, minZ, maxX, maxZ := b[0], b[2], b[3], b[5]
		switch s.Facing {
		case cube.South:
			minX, minZ, maxX, maxZ = 16-maxX, 16-maxZ, 16-minX, 16-minZ
		case cube.East:
			minX, minZ, maxX, maxZ = 16-maxZ, minX, 16-minZ, maxX
		case cube.West:
			minX, minZ, maxX, maxZ = minZ, 16-maxX, maxZ, 16-minX
		}
		out = append(out, cube.Box(minX/16, b[1]/16, minZ/16, maxX/16, b[4]/16, maxZ/16))
	}
	return out
}

// FaceSolid reports only the back of the shelf as solid.
func (s Shelf) FaceSolid(_ cube.Pos, face cube.Face, _ world.BlockSource) bool {
	return face == s.Facing.Opposite().Face()
}
