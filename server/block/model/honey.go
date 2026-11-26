package model

import (
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/world"
)

// Honey has Bedrock's horizontal collision inset and full block height.
type Honey struct{ Solid }

func (Honey) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	return []cube.BBox{cube.Box(1.0/16, 0, 1.0/16, 15.0/16, 1, 15.0/16)}
}

// StaticCollisionKind reports that Honey has a reduced collision box rather than a full 1x1x1 cube.
func (Honey) StaticCollisionKind() world.CollisionKind {
	return world.CollisionDynamic
}
