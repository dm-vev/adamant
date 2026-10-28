package model

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Honey has Bedrock's horizontal collision inset and full block height.
type Honey struct{ Solid }

func (Honey) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	return []cube.BBox{cube.Box(1.0/16, 0, 1.0/16, 15.0/16, 1, 15.0/16)}
}
