package model

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Stair is the persisted collision and solid-face model for a stair block.
type Stair struct {
	// Facing specifies the direction that the full side of the Stair faces.
	Facing cube.Direction
	// UpsideDown turns the Stair upside-down, meaning the full side of the Stair is turned to the top side of the
	// block.
	UpsideDown bool
	// Corner specifies the encoded corner shape.
	Corner StairCorner
}

// StairCorner is the corner geometry stored by the game.
type StairCorner uint8

const (
	StairCornerNone StairCorner = iota
	StairCornerInnerLeft
	StairCornerInnerRight
	StairCornerOuterLeft
	StairCornerOuterRight
)

// Uint8 returns the compact representation used by block hashes.
func (c StairCorner) Uint8() uint8 { return uint8(c) }

// String returns the canonical minecraft:corner property value.
func (c StairCorner) String() string {
	switch c {
	case StairCornerNone:
		return "none"
	case StairCornerInnerLeft:
		return "inner_left"
	case StairCornerInnerRight:
		return "inner_right"
	case StairCornerOuterLeft:
		return "outer_left"
	case StairCornerOuterRight:
		return "outer_right"
	default:
		panic("invalid stair corner")
	}
}

// BBox returns the collision boxes for the encoded orientation and corner.
func (s Stair) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	count := 2
	if s.Corner == StairCornerInnerLeft || s.Corner == StairCornerInnerRight {
		count = 3
	}
	b := make([]cube.BBox, 1, count)
	b[0] = cube.Box(0, 0, 0, 1, 0.5, 1)
	if s.UpsideDown {
		b[0] = cube.Box(0, 0.5, 0, 1, 1, 1)
	}

	face, oppositeFace := s.Facing.Face(), s.Facing.Opposite().Face()
	switch s.Corner {
	case StairCornerNone, StairCornerInnerRight, StairCornerInnerLeft:
		b = append(b, cube.Box(0.5, 0.5, 0.5, 0.5, 1, 0.5).
			ExtendTowards(face, 0.5).
			Stretch(s.Facing.RotateRight().Face().Axis(), 0.5))
	}
	switch s.Corner {
	case StairCornerOuterLeft:
		b = append(b, cube.Box(0.5, 0.5, 0.5, 0.5, 1, 0.5).
			ExtendTowards(face, 0.5).
			ExtendTowards(s.Facing.RotateLeft().Face(), 0.5))
	case StairCornerOuterRight:
		b = append(b, cube.Box(0.5, 0.5, 0.5, 0.5, 1, 0.5).
			ExtendTowards(face, 0.5).
			ExtendTowards(s.Facing.RotateRight().Face(), 0.5))
	case StairCornerInnerRight:
		b = append(b, cube.Box(0.5, 0.5, 0.5, 0.5, 1, 0.5).
			ExtendTowards(oppositeFace, 0.5).
			ExtendTowards(s.Facing.RotateRight().Face(), 0.5))
	case StairCornerInnerLeft:
		b = append(b, cube.Box(0.5, 0.5, 0.5, 0.5, 1, 0.5).
			ExtendTowards(oppositeFace, 0.5).
			ExtendTowards(s.Facing.RotateLeft().Face(), 0.5))
	}
	if s.UpsideDown {
		for i := range b[1:] {
			b[i+1] = b[i+1].Translate(mgl64.Vec3{0, -0.5})
		}
	}
	return b
}

// RayBBox retains the straight stair outline used by BDS rays and projectiles,
// independently of the corner geometry used for entity collision.
func (s Stair) RayBBox(pos cube.Pos, bs world.BlockSource) []cube.BBox {
	s.Corner = StairCornerNone
	return s.BBox(pos, bs)
}

// FaceSolid returns true for all faces of the Stair that are completely filled.
func (s Stair) FaceSolid(_ cube.Pos, face cube.Face, _ world.BlockSource) bool {
	// Stairs always have a closed side at the top or bottom based on their orientation.
	if (face == cube.FaceUp && s.UpsideDown) || (face == cube.FaceDown && !s.UpsideDown) {
		return true
	}

	switch s.Corner {
	case StairCornerOuterRight, StairCornerOuterLeft:
		// Small corner blocks, they do not block water flowing out horizontally.
		return false
	case StairCornerNone:
		// Not a corner, so only block directly behind the stairs.
		return s.Facing.Face() == face
	case StairCornerInnerRight:
		return face == s.Facing.RotateRight().Face() || face == s.Facing.Face()
	default:
		return face == s.Facing.RotateLeft().Face() || face == s.Facing.Face()
	}
}
