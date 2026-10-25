package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// spreadingDirtTick ports SpreadingSnowyDirtBlock.randomTick, which grass and
// mycelium share: smothered, the block turns into dirt; bright enough above,
// it spreads to four random dirt blocks around it that it could cover.
func spreadingDirtTick(pos cube.Pos, tx *world.Tx, r *rand.Rand, self world.Block) {
	if !canBeGrass(tx, pos) {
		tx.SetBlock(pos, Dirt{}, nil)
		return
	}
	if tx.RawBrightness(pos.Side(cube.FaceUp)) < 9 {
		return
	}
	for range 4 {
		test := pos.Add(cube.Pos{r.IntN(3) - 1, r.IntN(5) - 3, r.IntN(3) - 1})
		if dirt, ok := tx.Block(test).(Dirt); !ok || dirt.Coarse {
			continue
		}
		if canBeGrass(tx, test) && !waterFluidAt(tx, test.Side(cube.FaceUp)) {
			tx.SetBlock(test, self, nil)
		}
	}
}

// canBeGrass ports SpreadingSnowyDirtBlock.canBeGrass: a single snow layer
// does not smother grass, a full water block does, and so does any block
// that lets no light through into it.
func canBeGrass(tx *world.Tx, pos cube.Pos) bool {
	above := pos.Side(cube.FaceUp)
	if snow, ok := tx.Block(above).(SnowLayer); ok && snow.layers() == 1 {
		return true
	}
	if l, ok := tx.Liquid(above); ok && (l.LiquidDepth() == 8 || l.LiquidFalling()) {
		// FluidState.getAmount of a source or falling fluid is 8.
		return false
	}
	return tx.LightBlockedInto(pos, cube.FaceUp) < 15
}

// waterFluidAt reports if the fluid at pos is water.
func waterFluidAt(tx *world.Tx, pos cube.Pos) bool {
	l, ok := tx.Liquid(pos)
	return ok && l.LiquidType() == "water"
}

// RandomTick ports MyceliumBlock, a SpreadingSnowyDirtBlock.
func (m Mycelium) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	spreadingDirtTick(pos, tx, r, m)
}

// RandomTick ports NyliumBlock.randomTick: nylium covered by a block that
// lets no light through turns back into netherrack.
func (n Nylium) RandomTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if tx.LightBlockedInto(pos, cube.FaceUp) >= 15 {
		tx.SetBlock(pos, Netherrack{}, nil)
	}
}
