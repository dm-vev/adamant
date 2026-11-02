package model

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Door is a model used for doors. It has no solid faces and a bounding box that changes depending on
// the direction of the door, whether it is open, and the side of its hinge.
type Door struct {
	// Kind identifies the concrete door, including its wood or copper variant.
	// A different door type cannot supply the paired half's properties.
	Kind string
	// Facing is the direction that the door is facing when closed.
	Facing cube.Direction
	// Open specifies if the Door is open. The direction it opens towards depends on the Right field.
	Open bool
	// Right specifies the attachment side of the door and, with that, the direction it opens in.
	Right bool
	// Top reads direction and opening from the lower half. Both halves read
	// the hinge from the upper half; unused raw half properties are retained.
	Top bool
}

// BBox returns a physics.BBox that depends on if the Door is open, what direction it is facing and whether it is
// attached to the right/left side of a block.
func (d Door) BBox(pos cube.Pos, source world.BlockSource) []cube.BBox {
	face := cube.FaceUp
	if d.Top {
		face = cube.FaceDown
	}
	if other, ok := source.Block(pos.Side(face)).Model().(Door); ok && other.Kind == d.Kind {
		if d.Top {
			d.Facing, d.Open = other.Facing, other.Open
		} else {
			d.Right = other.Right
		}
	} else {
		// Native getDoorBlocks checks concrete type equality, without checking
		// the partner's half bit. Missing or mixed pairs use direction zero,
		// closed and left hinge, independently of the retained raw properties.
		d.Facing, d.Open, d.Right = cube.East, false, false
	}
	if d.Open {
		if d.Right {
			return []cube.BBox{full.ExtendTowards(d.Facing.RotateLeft().Face(), -0.8125)}
		}
		return []cube.BBox{full.ExtendTowards(d.Facing.RotateRight().Face(), -0.8125)}
	}
	return []cube.BBox{full.ExtendTowards(d.Facing.Face(), -0.8125)}
}

// FaceSolid always returns false.
func (d Door) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool {
	return false
}
