package overworld

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/world"
)

// kelpFloorHolds reports whether a record may write the state after at p.
// Anything but kelp may. Kelp may if the cell below it holds kelp, or a floor
// that holds a column: a block with a sturdy top that is not in the block tag
// cannot_support_kelp, the test of the kelp features and of
// GrowingPlantBlock.canSurvive.
//
// Java places a kelp column one feature at a time against the chunk as the
// features before it left it, and a column whose first cell is taken, as by
// seagrass, is never placed. A record is computed against the base terrain,
// and applies cell by cell wherever the chunk is still as the record found
// it: a column whose foot another record took would keep the cells above and
// float on the seagrass. Each cell asks for its floor, so such a column is
// dropped whole.
func (r *featureRegion) kelpFloorHolds(p featurePos, after world.NativeState) bool {
	registry := r.c.BlockRegistry()
	if !isKelpID(after.CanonicalID(registry)) {
		return true
	}
	below := r.nativeState(featurePos{p.x, p.y - 1, p.z}).CanonicalID(registry)
	if isKelpID(below) {
		return true
	}
	shape, err := gamedata.BlockGenerationShapeID(below)
	return err == nil && shape.Sturdy&(1<<uint(cube.FaceUp)) != 0 && !featureBlockTagContainsID("cannot_support_kelp", below)
}

// isKelpID reports whether the source state ID id is kelp or its plant.
func isKelpID(id uint32) bool {
	name, _ := gamedata.BlockStateName(id)
	return name == "minecraft:kelp" || name == "minecraft:kelp_plant"
}
