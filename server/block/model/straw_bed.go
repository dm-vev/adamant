package model

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// StrawBed retains Source's quarter-block base and directional head pillow.
type StrawBed struct {
	Facing cube.Direction
	Head   bool
}

var strawBedFoot = [...]cube.BBox{cube.Box(0, 0, 0, 1, .25, 1)}
var strawBedHeads = [4][2]cube.BBox{
	cube.North: {cube.Box(0, 0, 0, 1, .25, 1), cube.Box(0, .25, 0, 1, .3125, .5)},
	cube.South: {cube.Box(0, 0, 0, 1, .25, 1), cube.Box(0, .25, .5, 1, .3125, 1)},
	cube.West:  {cube.Box(0, 0, 0, 1, .25, 1), cube.Box(0, .25, 0, .5, .3125, 1)},
	cube.East:  {cube.Box(0, 0, 0, 1, .25, 1), cube.Box(.5, .25, 0, 1, .3125, 1)},
}

func (b StrawBed) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	if !b.Head {
		return strawBedFoot[:]
	}
	return strawBedHeads[b.Facing][:]
}
func (StrawBed) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool { return false }
