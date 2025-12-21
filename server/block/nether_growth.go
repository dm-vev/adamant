package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Nylium is a nether block that supports nether vegetation.
type Nylium struct {
	solid
	bassDrum

	// Warped specifies if the nylium is warped. If false, crimson nylium is used.
	Warped bool
}

// SoilFor ...
func (n Nylium) SoilFor(block world.Block) bool {
	switch block.(type) {
	case NetherSprouts, CrimsonRoots, WarpedRoots:
		return true
	}
	return false
}

// BreakInfo ...
func (n Nylium) BreakInfo() BreakInfo {
	return newBreakInfo(0.4, pickaxeHarvestable, pickaxeEffective, oneOf(n))
}

// EncodeItem ...
func (n Nylium) EncodeItem() (name string, meta int16) {
	if n.Warped {
		return "minecraft:warped_nylium", 0
	}
	return "minecraft:crimson_nylium", 0
}

// EncodeBlock ...
func (n Nylium) EncodeBlock() (string, map[string]any) {
	if n.Warped {
		return "minecraft:warped_nylium", nil
	}
	return "minecraft:crimson_nylium", nil
}

// CrimsonRoots are a nether plant that grows on nylium.
type CrimsonRoots struct {
	transparent
	replaceable
	empty
}

// NeighbourUpdateTick ...
func (r CrimsonRoots) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !supportsVegetation(r, tx.Block(pos.Side(cube.FaceDown))) {
		breakBlock(r, pos, tx)
	}
}

// UseOnBlock ...
func (r CrimsonRoots) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, r)
	if !used {
		return false
	}
	if !supportsVegetation(r, tx.Block(pos.Side(cube.FaceDown))) {
		return false
	}

	place(tx, pos, r, user, ctx)
	return placed(ctx)
}

// HasLiquidDrops ...
func (CrimsonRoots) HasLiquidDrops() bool {
	return false
}

// FlammabilityInfo ...
func (CrimsonRoots) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(0, 0, true)
}

// BreakInfo ...
func (r CrimsonRoots) BreakInfo() BreakInfo {
	return newBreakInfo(0, func(t item.Tool) bool {
		return t.ToolType() == item.TypeShears
	}, nothingEffective, oneOf(r))
}

// CompostChance ...
func (CrimsonRoots) CompostChance() float64 {
	return 0.5
}

// EncodeItem ...
func (CrimsonRoots) EncodeItem() (name string, meta int16) {
	return "minecraft:crimson_roots", 0
}

// EncodeBlock ...
func (CrimsonRoots) EncodeBlock() (string, map[string]any) {
	return "minecraft:crimson_roots", nil
}

// WarpedRoots are a nether plant that grows on nylium.
type WarpedRoots struct {
	transparent
	replaceable
	empty
}

// NeighbourUpdateTick ...
func (r WarpedRoots) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !supportsVegetation(r, tx.Block(pos.Side(cube.FaceDown))) {
		breakBlock(r, pos, tx)
	}
}

// UseOnBlock ...
func (r WarpedRoots) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, r)
	if !used {
		return false
	}
	if !supportsVegetation(r, tx.Block(pos.Side(cube.FaceDown))) {
		return false
	}

	place(tx, pos, r, user, ctx)
	return placed(ctx)
}

// HasLiquidDrops ...
func (WarpedRoots) HasLiquidDrops() bool {
	return false
}

// FlammabilityInfo ...
func (WarpedRoots) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(0, 0, true)
}

// BreakInfo ...
func (r WarpedRoots) BreakInfo() BreakInfo {
	return newBreakInfo(0, func(t item.Tool) bool {
		return t.ToolType() == item.TypeShears
	}, nothingEffective, oneOf(r))
}

// CompostChance ...
func (WarpedRoots) CompostChance() float64 {
	return 0.5
}

// EncodeItem ...
func (WarpedRoots) EncodeItem() (name string, meta int16) {
	return "minecraft:warped_roots", 0
}

// EncodeBlock ...
func (WarpedRoots) EncodeBlock() (string, map[string]any) {
	return "minecraft:warped_roots", nil
}

// DirtWithRoots is a dirt variant with roots.
type DirtWithRoots struct {
	solid
}

// SoilFor ...
func (d DirtWithRoots) SoilFor(block world.Block) bool {
	switch block.(type) {
	case ShortGrass, Fern, DoubleTallGrass, Flower, DoubleFlower, NetherSprouts, PinkPetals, SugarCane, DeadBush:
		return true
	}
	return false
}

// BreakInfo ...
func (d DirtWithRoots) BreakInfo() BreakInfo {
	return newBreakInfo(0.5, alwaysHarvestable, shovelEffective, oneOf(d))
}

// EncodeItem ...
func (DirtWithRoots) EncodeItem() (name string, meta int16) {
	return "minecraft:dirt_with_roots", 0
}

// EncodeBlock ...
func (DirtWithRoots) EncodeBlock() (string, map[string]any) {
	return "minecraft:dirt_with_roots", nil
}
