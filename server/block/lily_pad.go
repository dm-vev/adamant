package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// LilyPad is a short, flat solid block that can be found naturally growing only on water, in swamps and wheat
// farm rooms in woodland mansions.
type LilyPad struct {
	transparent
}

// HasLiquidDrops ...
func (LilyPad) HasLiquidDrops() bool {
	return true
}

// EntityInside ports LilyPadBlock.entityInside: boats break lily pads they
// run into.
func (l LilyPad) EntityInside(pos cube.Pos, tx *world.Tx, e world.Entity) {
	switch e.H().Type().EncodeEntity() {
	case "minecraft:boat", "minecraft:chest_boat":
		breakBlock(l, pos, tx)
	}
}

// NeighbourUpdateTick ...
func (l LilyPad) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !lilyPadSupported(tx, pos) {
		breakBlock(l, pos, tx)
	}
}

// lilyPadSupported ports LilyPadBlock.mayPlaceOn: a water source or ice or
// frosted ice below, supports_lily_pad, and no fluid in the block itself.
func lilyPadSupported(tx *world.Tx, pos cube.Pos) bool {
	below := pos.Side(cube.FaceDown)
	_, ice := tx.Block(below).(Ice)
	_, frosted := tx.Block(below).(FrostedIce)
	if !waterSourceAt(tx, below) && !ice && !frosted {
		return false
	}
	_, liquid := tx.Liquid(pos)
	return !liquid
}

// UseOnBlock places the lily pad on the water the user looks at, as
// PlaceOnWaterBlockItem.
func (l LilyPad) UseOnBlock(_ cube.Pos, _ cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	return placeOnWater(tx, user, ctx, l, lilyPadSupported)
}

// Use places the lily pad on the water the user looks at.
func (l LilyPad) Use(tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	return placeOnWater(tx, user, ctx, l, lilyPadSupported)
}

// BreakInfo ...
func (l LilyPad) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(l))
}

// EncodeItem ...
func (LilyPad) EncodeItem() (name string, meta int16) {
	return "minecraft:waterlily", 0
}

// Model ...
func (LilyPad) Model() world.BlockModel {
	return model.LilyPad{}
}

// EncodeBlock ...
func (LilyPad) EncodeBlock() (string, map[string]any) {
	return "minecraft:waterlily", nil
}
