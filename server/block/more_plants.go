package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// GoldenDandelion is a decorative flower variant.
type GoldenDandelion struct {
	empty
	transparent
}

// NeighbourUpdateTick ...
func (g GoldenDandelion) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !supportsVegetation(g, tx.Block(pos.Side(cube.FaceDown))) {
		breakBlock(g, pos, tx)
	}
}

// UseOnBlock ...
func (g GoldenDandelion) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, g)
	if !used || !supportsVegetation(g, tx.Block(pos.Side(cube.FaceDown))) {
		return false
	}
	place(tx, pos, g, user, ctx)
	return placed(ctx)
}

// HasLiquidDrops ...
func (GoldenDandelion) HasLiquidDrops() bool { return true }

// FlammabilityInfo ...
func (GoldenDandelion) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(60, 100, false)
}

// BreakInfo ...
func (g GoldenDandelion) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(g))
}

// CompostChance ...
func (GoldenDandelion) CompostChance() float64 { return 0.65 }

// EncodeItem ...
func (GoldenDandelion) EncodeItem() (string, int16) { return "minecraft:golden_dandelion", 0 }

// EncodeBlock ...
func (GoldenDandelion) EncodeBlock() (string, map[string]any) {
	return "minecraft:golden_dandelion", nil
}

// BrownMushroom is a decorative mushroom block.
type BrownMushroom struct {
	empty
	transparent
}

// NeighbourUpdateTick ...
func (m BrownMushroom) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !supportsVegetation(m, tx.Block(pos.Side(cube.FaceDown))) {
		breakBlock(m, pos, tx)
	}
}

// UseOnBlock ...
func (m BrownMushroom) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, m)
	if !used || !supportsVegetation(m, tx.Block(pos.Side(cube.FaceDown))) {
		return false
	}
	place(tx, pos, m, user, ctx)
	return placed(ctx)
}

// HasLiquidDrops ...
func (BrownMushroom) HasLiquidDrops() bool { return true }

// FlammabilityInfo ...
func (BrownMushroom) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(60, 100, false)
}

// BreakInfo ...
func (m BrownMushroom) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(m))
}

// CompostChance ...
func (BrownMushroom) CompostChance() float64 { return 0.65 }

// EncodeItem ...
func (BrownMushroom) EncodeItem() (string, int16) { return "minecraft:brown_mushroom", 0 }

// EncodeBlock ...
func (BrownMushroom) EncodeBlock() (string, map[string]any) { return "minecraft:brown_mushroom", nil }

// RedMushroom is a decorative mushroom block.
type RedMushroom struct {
	empty
	transparent
}

// NeighbourUpdateTick ...
func (m RedMushroom) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !supportsVegetation(m, tx.Block(pos.Side(cube.FaceDown))) {
		breakBlock(m, pos, tx)
	}
}

// UseOnBlock ...
func (m RedMushroom) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, m)
	if !used || !supportsVegetation(m, tx.Block(pos.Side(cube.FaceDown))) {
		return false
	}
	place(tx, pos, m, user, ctx)
	return placed(ctx)
}

// HasLiquidDrops ...
func (RedMushroom) HasLiquidDrops() bool { return true }

// FlammabilityInfo ...
func (RedMushroom) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(60, 100, false)
}

// BreakInfo ...
func (m RedMushroom) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(m))
}

// CompostChance ...
func (RedMushroom) CompostChance() float64 { return 0.65 }

// EncodeItem ...
func (RedMushroom) EncodeItem() (string, int16) { return "minecraft:red_mushroom", 0 }

// EncodeBlock ...
func (RedMushroom) EncodeBlock() (string, map[string]any) { return "minecraft:red_mushroom", nil }

// Azalea is a decorative shrub.
type Azalea struct {
	transparent
	replaceable
	empty
}

// NeighbourUpdateTick ...
func (a Azalea) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !supportsVegetation(a, tx.Block(pos.Side(cube.FaceDown))) {
		breakBlock(a, pos, tx)
	}
}

// UseOnBlock ...
func (a Azalea) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, a)
	if !used || !supportsVegetation(a, tx.Block(pos.Side(cube.FaceDown))) {
		return false
	}
	place(tx, pos, a, user, ctx)
	return placed(ctx)
}

// HasLiquidDrops ...
func (Azalea) HasLiquidDrops() bool { return true }

// FlammabilityInfo ...
func (Azalea) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(60, 100, false)
}

// BreakInfo ...
func (a Azalea) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(a))
}

// CompostChance ...
func (Azalea) CompostChance() float64 { return 0.65 }

// EncodeItem ...
func (Azalea) EncodeItem() (string, int16) { return "minecraft:azalea", 0 }

// EncodeBlock ...
func (Azalea) EncodeBlock() (string, map[string]any) { return "minecraft:azalea", nil }

// FloweringAzalea is a flowering decorative shrub.
type FloweringAzalea struct {
	transparent
	replaceable
	empty
}

// NeighbourUpdateTick ...
func (a FloweringAzalea) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !supportsVegetation(a, tx.Block(pos.Side(cube.FaceDown))) {
		breakBlock(a, pos, tx)
	}
}

// UseOnBlock ...
func (a FloweringAzalea) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, a)
	if !used || !supportsVegetation(a, tx.Block(pos.Side(cube.FaceDown))) {
		return false
	}
	place(tx, pos, a, user, ctx)
	return placed(ctx)
}

// HasLiquidDrops ...
func (FloweringAzalea) HasLiquidDrops() bool { return true }

// FlammabilityInfo ...
func (FloweringAzalea) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(60, 100, false)
}

// BreakInfo ...
func (a FloweringAzalea) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(a))
}

// CompostChance ...
func (FloweringAzalea) CompostChance() float64 { return 0.65 }

// EncodeItem ...
func (FloweringAzalea) EncodeItem() (string, int16) { return "minecraft:flowering_azalea", 0 }

// EncodeBlock ...
func (FloweringAzalea) EncodeBlock() (string, map[string]any) {
	return "minecraft:flowering_azalea", nil
}
