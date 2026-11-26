package model

import (
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/world"
)

// DaylightDetector is the three-eighths-high detector body.
type DaylightDetector struct{}

func (DaylightDetector) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	return []cube.BBox{cube.Box(0, 0, 0, 1, 3.0/8, 1)}
}

func (DaylightDetector) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool { return false }
