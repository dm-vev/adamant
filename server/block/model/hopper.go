package model

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Hopper is a model used by hoppers.
type Hopper struct{ Facing cube.Face }

// BBox follows HopperBlock::addAABBs, including the neck and outlet.
func (h Hopper) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	bbox := []cube.BBox{
		cube.Box(0, .625, 0, 1, .6875, 1),
		cube.Box(0, .6875, 0, .125, 1, 1),
		cube.Box(.875, .6875, 0, 1, 1, 1),
		cube.Box(.125, .6875, 0, .875, 1, .125),
		cube.Box(.125, .6875, .875, .875, 1, 1),
		cube.Box(.25, .25, .25, .75, .625, .75),
	}
	spout := cube.Box(.375, 0, .375, .625, .25, .625)
	switch h.Facing {
	case cube.FaceNorth:
		spout = cube.Box(.375, .25, 0, .625, .5, .25)
	case cube.FaceSouth:
		spout = cube.Box(.375, .25, .75, .625, .5, 1)
	case cube.FaceWest:
		spout = cube.Box(0, .25, .375, .25, .5, .625)
	case cube.FaceEast:
		spout = cube.Box(.75, .25, .375, 1, .5, .625)
	}
	return append(bbox, spout)
}

// FaceSolid only returns true for the top face of the hopper.
func (Hopper) FaceSolid(_ cube.Pos, face cube.Face, _ world.BlockSource) bool {
	return face == cube.FaceUp
}
