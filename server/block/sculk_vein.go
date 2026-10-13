package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// SculkVein is a thin layer of sculk covering one or more faces of the blocks
// around it. Sculk charges from a sculk catalyst spread veins and turn the
// blocks beneath them into sculk.
type SculkVein struct {
	transparent
	empty
	sourceWaterDisplacer

	// Faces are the sides of the vein's space it covers.
	Faces MultiFace
}

func (v SculkVein) multiFaces() MultiFace                  { return v.Faces }
func (v SculkVein) withMultiFaces(f MultiFace) world.Block { v.Faces = f; return v }

// SideClosed lets water flow through the vein's empty collision shape.
func (SculkVein) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool { return false }

// ReplaceableBy ports MultifaceBlock.canBeReplaced: any other block replaces a
// vein, while another vein only fills a vacant face.
func (v SculkVein) ReplaceableBy(b world.Block) bool {
	_, vein := b.(SculkVein)
	return !vein || v.Faces != 63
}

// BreakInfo ...
func (v SculkVein) BreakInfo() BreakInfo {
	return newBreakInfo(0.2, alwaysHarvestable, hoeEffective, func(_ item.Tool, enchantments []item.Enchantment) []item.Stack {
		if !hasSilkTouch(enchantments) || v.Faces == 0 {
			return nil
		}
		return []item.Stack{item.NewStack(SculkVein{}, v.Faces.Count())}
	})
}

// UseOnBlock attaches a vein face to the clicked block side.
func (v SculkVein) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	return placeMultiface(pos, face, tx, user, ctx, sculkVeinSpreader)
}

// NeighbourUpdateTick drops faces whose supporting block is gone.
func (v SculkVein) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	updateMultiface(v, pos, tx)
}

// EncodeItem ...
func (SculkVein) EncodeItem() (name string, meta int16) {
	return "minecraft:sculk_vein", 0
}

// EncodeBlock ...
func (v SculkVein) EncodeBlock() (string, map[string]any) {
	return "minecraft:sculk_vein", map[string]any{"multi_face_direction_bits": int32(v.Faces)}
}

// allSculkVeins returns every sculk vein state.
func allSculkVeins() (b []world.Block) {
	for _, faces := range allMultiFaces() {
		b = append(b, SculkVein{Faces: faces})
	}
	return
}

// placeMultiface places the item's multi-face block against the clicked face,
// adding the face to a block of the same kind already there.
func placeMultiface[T multifaceBlock](pos cube.Pos, face cube.Face, tx *world.Tx, user item.User, ctx *item.UseContext, spreader multifaceSpreader[T]) bool {
	pos, face, used := firstReplaceable(tx, pos, face, spreader.block)
	if !used {
		return false
	}
	b, ok := spreader.stateForPlacement(tx, tx.Block(pos), pos, face.Opposite())
	if !ok {
		return false
	}
	place(tx, pos, b, user, ctx)
	return placed(ctx)
}

// updateMultiface ports MultifaceBlock.updateShape: faces that lost their
// support are removed and a block without faces is destroyed.
func updateMultiface(b multifaceBlock, pos cube.Pos, tx *world.Tx) {
	faces := multifaceSurvivingFaces(tx, pos, b.multiFaces())
	if faces == b.multiFaces() {
		return
	}
	if faces == 0 {
		breakBlock(b, pos, tx)
		return
	}
	tx.SetBlock(pos, b.withMultiFaces(faces), nil)
}
