package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"time"
)

// BambooPlanks are building blocks crafted from bamboo.
type BambooPlanks struct {
	solid
	bass
}

// FlammabilityInfo ...
func (BambooPlanks) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(5, 20, true)
}

// BreakInfo ...
func (BambooPlanks) BreakInfo() BreakInfo {
	return newBreakInfo(2, alwaysHarvestable, axeEffective, oneOf(BambooPlanks{})).withBlastResistance(15)
}

// RepairsWoodTools ...
func (BambooPlanks) RepairsWoodTools() bool {
	return true
}

// FuelInfo ...
func (BambooPlanks) FuelInfo() item.FuelInfo {
	return newFuelInfo(time.Second * 15)
}

// EncodeItem ...
func (BambooPlanks) EncodeItem() (name string, meta int16) {
	return "minecraft:bamboo_planks", 0
}

// EncodeBlock ...
func (BambooPlanks) EncodeBlock() (name string, properties map[string]any) {
	return "minecraft:bamboo_planks", nil
}

// BambooMosaic is a decorative bamboo block.
type BambooMosaic struct {
	solid
	bass
}

// FlammabilityInfo ...
func (BambooMosaic) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(5, 20, true)
}

// BreakInfo ...
func (BambooMosaic) BreakInfo() BreakInfo {
	return newBreakInfo(2, alwaysHarvestable, axeEffective, oneOf(BambooMosaic{})).withBlastResistance(15)
}

// RepairsWoodTools ...
func (BambooMosaic) RepairsWoodTools() bool {
	return true
}

// FuelInfo ...
func (BambooMosaic) FuelInfo() item.FuelInfo {
	return newFuelInfo(time.Second * 15)
}

// EncodeItem ...
func (BambooMosaic) EncodeItem() (name string, meta int16) {
	return "minecraft:bamboo_mosaic", 0
}

// EncodeBlock ...
func (BambooMosaic) EncodeBlock() (name string, properties map[string]any) {
	return "minecraft:bamboo_mosaic", nil
}

// BambooBlock is a decorative bamboo block, with a stripped variant.
type BambooBlock struct {
	solid
	bass

	// Axis is the axis which the bamboo block faces.
	Axis cube.Axis
	// Stripped specifies if the bamboo block is stripped.
	Stripped bool
}

// FlammabilityInfo ...
func (BambooBlock) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(5, 5, true)
}

// BreakInfo ...
func (b BambooBlock) BreakInfo() BreakInfo {
	return newBreakInfo(2, alwaysHarvestable, axeEffective, oneOf(b))
}

// FuelInfo ...
func (BambooBlock) FuelInfo() item.FuelInfo {
	return newFuelInfo(time.Second * 15)
}

// UseOnBlock ...
func (b BambooBlock) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) (used bool) {
	pos, face, used = firstReplaceable(tx, pos, face, b)
	if !used {
		return
	}
	b.Axis = face.Axis()

	place(tx, pos, b, user, ctx)
	return placed(ctx)
}

// EncodeItem ...
func (b BambooBlock) EncodeItem() (name string, meta int16) {
	if b.Stripped {
		return "minecraft:stripped_bamboo_block", 0
	}
	return "minecraft:bamboo_block", 0
}

// EncodeBlock ...
func (b BambooBlock) EncodeBlock() (string, map[string]any) {
	if b.Stripped {
		return "minecraft:stripped_bamboo_block", map[string]any{"pillar_axis": b.Axis.String()}
	}
	return "minecraft:bamboo_block", map[string]any{"pillar_axis": b.Axis.String()}
}

// allBambooBlocks returns all bamboo block states.
func allBambooBlocks() (blocks []world.Block) {
	for _, axis := range cube.Axes() {
		blocks = append(blocks, BambooBlock{Axis: axis, Stripped: false})
		blocks = append(blocks, BambooBlock{Axis: axis, Stripped: true})
	}
	return
}

// BambooFence is a bamboo fence block.
type BambooFence struct {
	transparent
	bass
	sourceWaterDisplacer
}

// BreakInfo ...
func (BambooFence) BreakInfo() BreakInfo {
	return newBreakInfo(2, alwaysHarvestable, axeEffective, oneOf(BambooFence{})).withBlastResistance(15)
}

// FlammabilityInfo ...
func (BambooFence) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(5, 20, true)
}

// FuelInfo ...
func (BambooFence) FuelInfo() item.FuelInfo {
	return newFuelInfo(time.Second * 15)
}

// SideClosed ...
func (BambooFence) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// EncodeBlock ...
func (BambooFence) EncodeBlock() (name string, properties map[string]any) {
	return "minecraft:bamboo_fence", nil
}

// Model ...
func (BambooFence) Model() world.BlockModel {
	return model.Fence{Wood: true}
}

// EncodeItem ...
func (BambooFence) EncodeItem() (name string, meta int16) {
	return "minecraft:bamboo_fence", 0
}
