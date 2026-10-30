package model

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Bell is a model used by bells.
type Bell struct {
	// Attachment is the bell attachment type.
	Attachment BellAttachment
	// Facing is the bell's horizontal direction.
	Facing cube.Direction
}

// BellAttachment is the attachment variant used by the bell model.
type BellAttachment uint8

const (
	// BellAttachmentStanding is a floor-mounted bell.
	BellAttachmentStanding BellAttachment = iota
	// BellAttachmentHanging is a ceiling-mounted bell.
	BellAttachmentHanging
	// BellAttachmentSide is a wall-mounted bell with a single support.
	BellAttachmentSide
	// BellAttachmentMultiple is a wall-mounted bell with supports on both sides.
	BellAttachmentMultiple
)

// BBox ...
func (b Bell) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	switch b.Attachment {
	case BellAttachmentStanding:
		if b.Facing.Face().Axis() == cube.X {
			return []cube.BBox{cube.Box(0.25, 0, 0, 0.75, 0.8125, 1)}
		}
		return []cube.BBox{cube.Box(0, 0, 0.25, 1, 0.8125, 0.75)}
	case BellAttachmentHanging:
		return []cube.BBox{cube.Box(0.25, 0.25, 0.25, 0.75, 1, 0.75)}
	case BellAttachmentSide, BellAttachmentMultiple:
		if b.Attachment == BellAttachmentMultiple {
			if b.Facing.Face().Axis() == cube.X {
				return []cube.BBox{cube.Box(0, 0.25, 0.25, 1, 1, 0.75)}
			}
			return []cube.BBox{cube.Box(0.25, 0.25, 0, 0.75, 1, 1)}
		}
		switch b.Facing {
		case cube.North:
			return []cube.BBox{cube.Box(0.25, 0.25, 0.1875, 0.75, 0.9375, 1)}
		case cube.South:
			return []cube.BBox{cube.Box(0.25, 0.25, 0, 0.75, 0.9375, 0.8125)}
		case cube.West:
			return []cube.BBox{cube.Box(0.1875, 0.25, 0.25, 1, 0.9375, 0.75)}
		default:
			return []cube.BBox{cube.Box(0, 0.25, 0.25, 0.8125, 0.9375, 0.75)}
		}
	default:
		return nil
	}
}

// FaceSolid always returns false.
func (Bell) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool {
	return false
}
