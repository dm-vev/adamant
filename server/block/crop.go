package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Crop is an interface for all crops that are grown on farmland. A crop has a random chance to grow during random ticks.
type Crop interface {
	// GrowthStage returns the crop's current stage of growth. The max value is 7.
	GrowthStage() int
	// SameCrop checks if two crops are of the same type.
	SameCrop(c Crop) bool
}

// crop is a base for crop plants.
type crop struct {
	transparent
	empty

	// Growth is the current stage of growth. The max value is 7.
	Growth int
}

// NeighbourUpdateTick ...
func (c crop) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if _, ok := tx.Block(pos.Side(cube.FaceDown)).(Farmland); !ok {
		breakBlock(tx.Block(pos), pos, tx)
	}
}

// HasLiquidDrops ...
func (c crop) HasLiquidDrops() bool {
	return true
}

// GrowthStage returns the current stage of growth.
func (c crop) GrowthStage() int {
	return c.Growth
}

// CalculateGrowthChance calculates the chance the crop will grow during a random tick.
func (c crop) CalculateGrowthChance(pos cube.Pos, tx *world.Tx) float64 {
	block := tx.Block(pos)
	points := cropGrowthSpeed(pos, tx, func(b world.Block) bool { return sameCrop(block, b) })
	return 1 / (25/points + 1)
}

// cropGrowthSpeed ports CropBlock.getGrowthSpeed: farmland below the crop
// counts 4 when moist and 2 when dry, the eight farmland blocks around it a
// quarter of one less each, halved when the same crop grows in rows on both
// axes or diagonally next to it.
func cropGrowthSpeed(pos cube.Pos, tx *world.Tx, same func(world.Block) bool) float64 {
	points := 0.0
	under := pos.Side(cube.FaceDown)
	for x := -1; x <= 1; x++ {
		for z := -1; z <= 1; z++ {
			if farmland, ok := tx.Block(under.Add(cube.Pos{x, 0, z})).(Farmland); ok {
				farmlandPoints := 2.0
				if farmland.Hydration > 0 {
					farmlandPoints = 4
				}
				if x != 0 || z != 0 {
					farmlandPoints = (farmlandPoints - 1) / 4
				}
				points += farmlandPoints
			}
		}
	}

	north := pos.Side(cube.FaceNorth)
	south := pos.Side(cube.FaceSouth)

	northSouth := same(tx.Block(north)) || same(tx.Block(south))
	westEast := same(tx.Block(pos.Side(cube.FaceWest))) || same(tx.Block(pos.Side(cube.FaceEast)))
	if northSouth && westEast {
		points /= 2
	} else if same(tx.Block(north.Side(cube.FaceWest))) || same(tx.Block(north.Side(cube.FaceEast))) ||
		same(tx.Block(south.Side(cube.FaceWest))) || same(tx.Block(south.Side(cube.FaceEast))) {
		points /= 2
	}
	return points
}

// sameCrop checks if both blocks are crops and that they are the same type.
func sameCrop(blockA, blockB world.Block) bool {
	if a, ok := blockA.(Crop); ok {
		if b, ok := blockB.(Crop); ok {
			return a.SameCrop(b)
		}
	}
	return false
}
