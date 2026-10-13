package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// onVegetationSoil ports VegetationBlock.mayPlaceOn: the block below pos is in
// #supports_vegetation.
func onVegetationSoil(tx *world.Tx, pos cube.Pos) bool {
	return javaBlockTag(tx, pos.Side(cube.FaceDown), "supports_vegetation")
}

// spreadToNeighbour ports the bone meal of BushBlock and FireflyBushBlock:
// the plant spreads to a random empty horizontal neighbour on vegetation
// soil.
func spreadToNeighbour(tx *world.Tx, pos cube.Pos, b world.Block) item.BoneMealResult {
	faces := cube.HorizontalFaces()
	for _, i := range rand.Perm(len(faces)) {
		neighbour := pos.Side(faces[i])
		if _, air := tx.Block(neighbour).(Air); air && onVegetationSoil(tx, neighbour) {
			tx.SetBlock(neighbour, b, nil)
			return item.BoneMealResultSmall
		}
	}
	return item.BoneMealResultNone
}

// RedShrub is a short red plant growing on soil.
type RedShrub struct {
	transparent
	replaceable
	empty
}

// NeighbourUpdateTick ...
func (r RedShrub) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !onVegetationSoil(tx, pos) {
		breakBlock(r, pos, tx)
	}
}

// UseOnBlock ...
func (r RedShrub) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, r)
	if !used || !onVegetationSoil(tx, pos) {
		return false
	}
	place(tx, pos, r, user, ctx)
	return placed(ctx)
}

// BoneMeal spreads the shrub to a free neighbour.
func (r RedShrub) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	return spreadToNeighbour(tx, pos, r)
}

// HasLiquidDrops ...
func (RedShrub) HasLiquidDrops() bool { return true }

// FlammabilityInfo ...
func (RedShrub) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(60, 100, true)
}

// BreakInfo ...
func (r RedShrub) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(r))
}

// CompostChance ...
func (RedShrub) CompostChance() float64 { return 0.3 }

// EncodeItem ...
func (RedShrub) EncodeItem() (name string, meta int16) {
	return "minecraft:red_shrub", 0
}

// EncodeBlock ...
func (RedShrub) EncodeBlock() (string, map[string]any) {
	return "minecraft:red_shrub", nil
}
