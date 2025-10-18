package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// EndPortalFrame is a decorative block that completes end portals when activated with Eyes of Ender.
type EndPortalFrame struct {
	solid

	// Facing is the direction the frame is facing.
	Facing cube.Direction
	// Eye specifies if an Eye of Ender has been placed into the frame.
	Eye bool
}

// UseOnBlock handles the placing of end portal frames and orients them based on the user facing direction.
func (f EndPortalFrame) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, f)
	if !used {
		return false
	}

	f.Facing = user.Rotation().Direction()
	place(tx, pos, f, user, ctx)
	return placed(ctx)
}

// BreakInfo returns information on how the frame should be broken.
func (f EndPortalFrame) BreakInfo() BreakInfo {
	return newBreakInfo(22.5, pickaxeHarvestable, pickaxeEffective, oneOf(f)).withBlastResistance(3600)
}

// EncodeItem returns the ID and metadata for the end portal frame item.
func (EndPortalFrame) EncodeItem() (name string, meta int16) {
	return "minecraft:end_portal_frame", 0
}

// EncodeBlock returns the block state for the frame.
func (f EndPortalFrame) EncodeBlock() (string, map[string]any) {
	return "minecraft:end_portal_frame", map[string]any{
		"minecraft:cardinal_direction": f.Facing.String(),
		"end_portal_eye_bit":           f.Eye,
	}
}

// allEndPortalFrames returns all block states for end portal frames.
func allEndPortalFrames() (frames []world.Block) {
	for _, d := range cube.Directions() {
		frames = append(frames, EndPortalFrame{Facing: d})
		frames = append(frames, EndPortalFrame{Facing: d, Eye: true})
	}
	return
}
