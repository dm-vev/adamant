package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// BambooSapling is a small bamboo plant that grows into bamboo.
type BambooSapling struct {
	empty
	transparent
	bass

	Ready bool
}

// BoneMeal attempts to affect the block using a bone meal item.
func (b BambooSapling) BoneMeal(pos cube.Pos, tx *world.Tx) bool {
	return b.grow(pos, tx)
}

// NeighbourUpdateTick ...
func (b BambooSapling) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	down := tx.Block(pos.Side(cube.FaceDown))
	if supportsVegetation(b, down) {
		return
	}
	breakBlock(b, pos, tx)
}

// RandomTick ...
func (b BambooSapling) RandomTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if b.Ready {
		if tx.Light(pos) < 9 || !b.grow(pos, tx) {
			b.Ready = false
			tx.SetBlock(pos, b, nil)
		}
	} else if replaceableWith(tx, pos.Side(cube.FaceUp), b) {
		b.Ready = true
		tx.SetBlock(pos, b, nil)
	}
}

// BreakInfo ...
func (b BambooSapling) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, axeEffective, oneOf(Bamboo{}))
}

// HasLiquidDrops ...
func (BambooSapling) HasLiquidDrops() bool {
	return true
}

// EncodeBlock ...
func (b BambooSapling) EncodeBlock() (string, map[string]any) {
	return "minecraft:bamboo_sapling", map[string]any{"age_bit": boolByte(b.Ready)}
}

func (b BambooSapling) grow(pos cube.Pos, tx *world.Tx) bool {
	if !replaceableWith(tx, pos.Side(cube.FaceUp), b) {
		return false
	}

	tx.SetBlock(pos, Bamboo{}, nil)
	tx.SetBlock(pos.Side(cube.FaceUp), Bamboo{LeafSize: BambooSizeSmallLeaves()}, nil)
	return true
}

// allBambooSaplings returns all possible states of a bamboo sapling block.
func allBambooSaplings() []world.Block {
	return []world.Block{
		BambooSapling{Ready: false},
		BambooSapling{Ready: true},
	}
}
