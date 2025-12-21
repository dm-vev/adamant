package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Eyeblossom is a decorative plant with open and closed variants.
type Eyeblossom struct {
	empty
	transparent

	Open bool
}

// NeighbourUpdateTick ...
func (e Eyeblossom) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !supportsVegetation(e, tx.Block(pos.Side(cube.FaceDown))) {
		breakBlock(e, pos, tx)
	}
}

// UseOnBlock ...
func (e Eyeblossom) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, e)
	if !used || !supportsVegetation(e, tx.Block(pos.Side(cube.FaceDown))) {
		return false
	}

	place(tx, pos, e, user, ctx)
	return placed(ctx)
}

// HasLiquidDrops ...
func (Eyeblossom) HasLiquidDrops() bool {
	return true
}

// FlammabilityInfo ...
func (Eyeblossom) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(60, 100, false)
}

// BreakInfo ...
func (e Eyeblossom) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(e))
}

// CompostChance ...
func (Eyeblossom) CompostChance() float64 {
	return 0.65
}

// EncodeItem ...
func (e Eyeblossom) EncodeItem() (name string, meta int16) {
	if e.Open {
		return "minecraft:open_eyeblossom", 0
	}
	return "minecraft:closed_eyeblossom", 0
}

// EncodeBlock ...
func (e Eyeblossom) EncodeBlock() (string, map[string]any) {
	if e.Open {
		return "minecraft:open_eyeblossom", nil
	}
	return "minecraft:closed_eyeblossom", nil
}
