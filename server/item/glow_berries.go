package item

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// GlowBerries is the native berry item dropped by cave vines. It is eaten, and
// planted as cave vines hanging from a block.
type GlowBerries struct {
	defaultFood
}

// Consume ports the food component of glow berries.
func (GlowBerries) Consume(_ *world.Tx, c Consumer) Stack {
	c.Saturate(2, 0.4)
	return Stack{}
}

// UseOnBlock plants cave vines, leaving placement to the cave vines block.
func (GlowBerries) UseOnBlock(pos cube.Pos, face cube.Face, clickPos mgl64.Vec3, tx *world.Tx, user User, ctx *UseContext) bool {
	vines, ok := world.BlockByName("minecraft:cave_vines", map[string]any{"growing_plant_age": int32(0)})
	if !ok {
		return false
	}
	usable, ok := vines.(UsableOnBlock)
	return ok && usable.UseOnBlock(pos, face, clickPos, tx, user, ctx)
}

// EncodeItem encodes the glow berries item.
func (GlowBerries) EncodeItem() (string, int16) { return "minecraft:glow_berries", 0 }
