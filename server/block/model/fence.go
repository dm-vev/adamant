package model

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Fence is a model used by fences of any type. It can attach to blocks with solid faces and other fences of the same
// type and has a model height just slightly over 1.
type Fence struct {
	// Wood specifies if the Fence is made from wood. This field is used to check if two fences are able to
	// attach to each other.
	Wood                     bool
	North, East, South, West bool
}

const (
	fenceHeight = 1.5
	fenceInset  = 0.375
)

// BBox returns multiple physics.BBox depending on how many connections it has with the surrounding blocks.
func (f Fence) BBox(_ cube.Pos, _ world.BlockSource) []cube.BBox {
	boxes := make([]cube.BBox, 0, 4)
	if f.West || f.East {
		sideBox := cube.Box(0, 0, 0, 1, fenceHeight, 1).Stretch(cube.Z, -fenceInset)
		if f.West {
			boxes = append(boxes, sideBox.ExtendTowards(cube.FaceEast, -fenceInset))
		}
		if f.East {
			boxes = append(boxes, sideBox.ExtendTowards(cube.FaceWest, -fenceInset))
		}
	}
	if f.North || f.South {
		sideBox := cube.Box(0, 0, 0, 1, fenceHeight, 1).Stretch(cube.X, -fenceInset)
		if f.North {
			boxes = append(boxes, sideBox.ExtendTowards(cube.FaceSouth, -fenceInset))
		}
		if f.South {
			boxes = append(boxes, sideBox.ExtendTowards(cube.FaceNorth, -fenceInset))
		}
	}
	if len(boxes) == 0 {
		boxes = append(boxes, cube.Box(fenceInset, 0, fenceInset, 1-fenceInset, fenceHeight, 1-fenceInset))
	}
	return boxes
}

// FaceSolid returns true if the face is cube.FaceDown or cube.FaceUp.
func (f Fence) FaceSolid(_ cube.Pos, face cube.Face, _ world.BlockSource) bool {
	return face == cube.FaceDown || face == cube.FaceUp
}
