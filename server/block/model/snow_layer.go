package model

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// SnowLayer is the model of a snow layer block. The amount of layers determines the height of the collision box.
// One layer is 1/8 of a block.
type SnowLayer struct {
	// Layers is the number of snow layers, in the range 1..8.
	Layers uint8
}

func (s SnowLayer) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	layers := s.Layers
	if layers < 1 {
		layers = 1
	}
	if layers > 8 {
		layers = 8
	}
	// SnowLayerBlock.getCollisionShape: a layer lower than the snow looks,
	// so that a single layer does not collide at all.
	if layers == 1 {
		return nil
	}
	height := float64(layers-1) / 8.0
	return []cube.BBox{cube.Box(0, 0, 0, 1, height, 1)}
}

func (s SnowLayer) FaceSolid(_ cube.Pos, face cube.Face, _ world.BlockSource) bool {
	layers := s.Layers
	if layers < 1 {
		layers = 1
	}
	if layers >= 8 {
		return true
	}
	// Snow layers only have a solid bottom face.
	return face == cube.FaceDown
}
