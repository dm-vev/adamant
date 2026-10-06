package model

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Thin is a model for thin, partial blocks such as a glass pane or an iron bar.
// Its connection fields are persisted block state and determine its geometry.
type Thin struct {
	North bool
	East  bool
	South bool
	West  bool
}

const (
	thinHeight = 1
	thinInset  = 7.0 / 16.0
)

// BBox returns collision boxes matching the persisted connections of the thin block.
func (t Thin) BBox(_ cube.Pos, _ world.BlockSource) []cube.BBox {
	boxes := make([]cube.BBox, 0, 2)

	if t.West || t.East {
		box := cube.Box(0, 0, 0, 1, thinHeight, 1).Stretch(cube.Z, -thinInset)
		if !t.West {
			box = box.ExtendTowards(cube.FaceWest, -thinInset)
		} else if !t.East {
			box = box.ExtendTowards(cube.FaceEast, -thinInset)
		}
		boxes = append(boxes, box)
	}

	if t.North || t.South {
		box := cube.Box(0, 0, 0, 1, thinHeight, 1).Stretch(cube.X, -thinInset)
		if !t.North {
			box = box.ExtendTowards(cube.FaceNorth, -thinInset)
		} else if !t.South {
			box = box.ExtendTowards(cube.FaceSouth, -thinInset)
		}
		boxes = append(boxes, box)
	}

	if len(boxes) == 0 {
		boxes = append(boxes, cube.Box(0, 0, 0, 1, thinHeight, 1).Stretch(cube.X, -thinInset).Stretch(cube.Z, -thinInset))
	}
	return boxes
}

// FaceSolid returns true if the face passed is cube.FaceDown.
func (Thin) FaceSolid(_ cube.Pos, face cube.Face, _ world.BlockSource) bool {
	return face == cube.FaceDown
}
