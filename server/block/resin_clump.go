package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// ResinClump is a multi-face block that creaking hearts grow on pale oak logs.
type ResinClump struct {
	transparent
	empty
	sourceWaterDisplacer

	// Faces are the sides of the clump's space it covers.
	Faces MultiFace
}

// resinClumpSpreader places resin clump faces for players.
var resinClumpSpreader = multifaceSpreader[ResinClump]{block: ResinClump{}, types: defaultMultifaceSpreadOrder}

func (r ResinClump) multiFaces() MultiFace                 { return r.Faces }
func (r ResinClump) withMultiFaces(f MultiFace) world.Block { r.Faces = f; return r }

// SideClosed allows water to flow through the clump's empty collision shape.
func (ResinClump) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool { return false }

var _ world.LiquidDisplacer = ResinClump{}

// ReplaceableBy ports MultifaceBlock.canBeReplaced: any other block replaces
// a clump, while more resin only fills a vacant face.
func (r ResinClump) ReplaceableBy(b world.Block) bool {
	_, resin := b.(ResinClump)
	return !resin || r.Faces != 63
}

// BreakInfo ports the instantly broken RESIN_CLUMP, which drops one clump per
// covered face with any tool.
func (r ResinClump) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, func(item.Tool, []item.Enchantment) []item.Stack {
		if r.Faces == 0 {
			return nil
		}
		return []item.Stack{item.NewStack(ResinClump{}, r.Faces.Count())}
	})
}

// UseOnBlock attaches a clump face to the clicked block side.
func (r ResinClump) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	return placeMultiface(pos, face, tx, user, ctx, resinClumpSpreader)
}

// NeighbourUpdateTick drops faces whose supporting block is gone.
func (r ResinClump) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	updateMultiface(r, pos, tx)
}

// EncodeItem ...
func (ResinClump) EncodeItem() (name string, meta int16) {
	return "minecraft:resin_clump", 0
}

// EncodeBlock ...
func (r ResinClump) EncodeBlock() (string, map[string]any) {
	return "minecraft:resin_clump", map[string]any{"multi_face_direction_bits": int32(r.Faces)}
}

// allResinClumps returns all resin clump states.
func allResinClumps() (b []world.Block) {
	for _, faces := range allMultiFaces() {
		b = append(b, ResinClump{Faces: faces})
	}
	return
}
