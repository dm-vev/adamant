package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// GlowLichen is a luminous multi-face growth found in caves. Bone meal spreads
// it to neighbouring faces.
type GlowLichen struct {
	transparent
	empty
	sourceWaterDisplacer

	// Faces are the sides of the lichen's space it covers.
	Faces MultiFace
}

// glowLichenSpreader is GlowLichenBlock's default MultifaceSpreader.
var glowLichenSpreader = multifaceSpreader[GlowLichen]{block: GlowLichen{}, types: defaultMultifaceSpreadOrder}

func (g GlowLichen) multiFaces() MultiFace                  { return g.Faces }
func (g GlowLichen) withMultiFaces(f MultiFace) world.Block { g.Faces = f; return g }

// SideClosed lets water flow through the lichen's empty collision shape.
func (GlowLichen) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool { return false }

// ReplaceableBy ports MultifaceBlock.canBeReplaced: any other block replaces
// lichen, while more lichen only fills a vacant face.
func (g GlowLichen) ReplaceableBy(b world.Block) bool {
	_, lichen := b.(GlowLichen)
	return !lichen || g.Faces != 63
}

// LightEmissionLevel ports GlowLichenBlock.emission(7).
func (g GlowLichen) LightEmissionLevel() uint8 {
	if g.Faces == 0 {
		return 0
	}
	return 7
}

// FlammabilityInfo ...
func (GlowLichen) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(15, 100, true)
}

// BreakInfo ...
func (g GlowLichen) BreakInfo() BreakInfo {
	return newBreakInfo(0.2, alwaysHarvestable, func(t item.Tool) bool {
		return t.ToolType() == item.TypeAxe || t.ToolType() == item.TypeShears
	}, func(t item.Tool, _ []item.Enchantment) []item.Stack {
		if t.ToolType() != item.TypeShears || g.Faces == 0 {
			return nil
		}
		return []item.Stack{item.NewStack(GlowLichen{}, g.Faces.Count())}
	})
}

// BoneMeal ports GlowLichenBlock.performBonemeal: the lichen grows onto one
// random reachable face.
func (g GlowLichen) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	valid := false
	for _, face := range cube.Faces() {
		if glowLichenSpreader.canSpreadInAnyDirection(tx, g, pos, face.Opposite()) {
			valid = true
			break
		}
	}
	if !valid {
		return item.BoneMealResultNone
	}
	glowLichenSpreader.spreadFromRandomFaceTowardRandomDirection(tx, g, pos, nil)
	return item.BoneMealResultSmall
}

// UseOnBlock attaches a lichen face to the clicked block side.
func (g GlowLichen) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	return placeMultiface(pos, face, tx, user, ctx, glowLichenSpreader)
}

// NeighbourUpdateTick drops faces whose supporting block is gone.
func (g GlowLichen) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	updateMultiface(g, pos, tx)
}

// EncodeItem ...
func (GlowLichen) EncodeItem() (name string, meta int16) {
	return "minecraft:glow_lichen", 0
}

// EncodeBlock ...
func (g GlowLichen) EncodeBlock() (string, map[string]any) {
	return "minecraft:glow_lichen", map[string]any{"multi_face_direction_bits": int32(g.Faces)}
}

// allGlowLichen returns every glow lichen state.
func allGlowLichen() (b []world.Block) {
	for _, faces := range allMultiFaces() {
		b = append(b, GlowLichen{Faces: faces})
	}
	return
}
