package model

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Lever is the block model for a lever, exposing a small collision box aligned with its mounting face.
type Lever struct {
	Face cube.Face
	Axis cube.Axis
}

var (
	leverFloorX  = cube.Box(0.1875, 0, 0.3125, 0.8125, 0.5625, 0.6875)
	leverFloorZ  = cube.Box(0.3125, 0, 0.1875, 0.6875, 0.5625, 0.8125)
	leverCeilX   = cube.Box(0.1875, 0.4375, 0.3125, 0.8125, 1, 0.6875)
	leverCeilZ   = cube.Box(0.3125, 0.4375, 0.1875, 0.6875, 1, 0.8125)
	leverNorth   = cube.Box(0.3125, 0.1875, 0, 0.6875, 0.8125, 0.5625)
	leverSouth   = cube.Box(0.3125, 0.1875, 0.4375, 0.6875, 0.8125, 1)
	leverWest    = cube.Box(0, 0.1875, 0.3125, 0.5625, 0.8125, 0.6875)
	leverEast    = cube.Box(0.4375, 0.1875, 0.3125, 1, 0.8125, 0.6875)
	defaultLever = cube.Box(0.3125, 0, 0.3125, 0.6875, 0.5625, 0.6875)
)

// BBox returns the oriented bounding box for the lever.
func (l Lever) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	switch l.Face {
	case cube.FaceUp:
		if l.Axis == cube.X {
			return []cube.BBox{leverFloorX}
		}
		return []cube.BBox{leverFloorZ}
	case cube.FaceDown:
		if l.Axis == cube.X {
			return []cube.BBox{leverCeilX}
		}
		return []cube.BBox{leverCeilZ}
	case cube.FaceNorth:
		return []cube.BBox{leverNorth}
	case cube.FaceSouth:
		return []cube.BBox{leverSouth}
	case cube.FaceWest:
		return []cube.BBox{leverWest}
	case cube.FaceEast:
		return []cube.BBox{leverEast}
	default:
		return []cube.BBox{defaultLever}
	}
}

// FaceSolid reports no solid faces, allowing other blocks to ignore it for support.
func (Lever) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool {
	return false
}
