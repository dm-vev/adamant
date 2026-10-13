package model

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Cauldron is the model of a cauldron: AbstractCauldronBlock.SHAPE, a full
// block hollowed out above a floor at four pixels, standing on four L-shaped
// corner legs three pixels tall.
type Cauldron struct{}

// BBox ...
func (Cauldron) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	const px = 1.0 / 16
	return []cube.BBox{
		// Floor and walls above the legs.
		cube.Box(0, 3*px, 0, 1, 4*px, 1),
		cube.Box(0, 3*px, 0, 2*px, 1, 1),
		cube.Box(14*px, 3*px, 0, 1, 1, 1),
		cube.Box(2*px, 3*px, 0, 14*px, 1, 2*px),
		cube.Box(2*px, 3*px, 14*px, 14*px, 1, 1),
		// Corner legs.
		cube.Box(0, 0, 0, 4*px, 3*px, 2*px),
		cube.Box(0, 0, 2*px, 2*px, 3*px, 4*px),
		cube.Box(12*px, 0, 0, 1, 3*px, 2*px),
		cube.Box(14*px, 0, 2*px, 1, 3*px, 4*px),
		cube.Box(0, 0, 14*px, 4*px, 3*px, 1),
		cube.Box(0, 0, 12*px, 2*px, 3*px, 14*px),
		cube.Box(12*px, 0, 14*px, 1, 3*px, 1),
		cube.Box(14*px, 0, 12*px, 1, 3*px, 14*px),
	}
}

// FaceSolid reports the full side walls as solid; the open top and the legged
// bottom are not.
func (Cauldron) FaceSolid(_ cube.Pos, face cube.Face, _ world.BlockSource) bool {
	return face != cube.FaceUp && face != cube.FaceDown
}
