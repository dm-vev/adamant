package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// DiscPress is a block Bedrock Edition 1.26.60 added, of no use yet: it has
// no name or texture of its own in the game, is not in the creative
// inventory, and does nothing. It faces one of the four horizontal
// directions. Clients of 1.26.50 are shown a stonecutter in its place.
type DiscPress struct {
	solid
	bassDrum

	// Facing is the direction the disc press faces.
	Facing cube.Direction
}

// BreakInfo is that of the stonecutter, which it stands in for: BDS does not
// tell the hardness of a block, and Java has no disc press.
func (d DiscPress) BreakInfo() BreakInfo {
	return newBreakInfo(3.5, pickaxeHarvestable, pickaxeEffective, oneOf(DiscPress{}))
}

// UseOnBlock places the disc press facing the player who places it.
func (d DiscPress) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) (used bool) {
	pos, _, used = firstReplaceable(tx, pos, face, d)
	if !used {
		return
	}
	d.Facing = user.Rotation().Direction().Opposite()
	place(tx, pos, d, user, ctx)
	return placed(ctx)
}

// EncodeItem ...
func (DiscPress) EncodeItem() (name string, meta int16) {
	return "minecraft:disc_press", 0
}

// EncodeBlock ...
func (d DiscPress) EncodeBlock() (string, map[string]any) {
	return "minecraft:disc_press", map[string]any{"minecraft:cardinal_direction": d.Facing.String()}
}

// allDiscPresses returns a disc press of every facing.
func allDiscPresses() (presses []world.Block) {
	for _, d := range cube.Directions() {
		presses = append(presses, DiscPress{Facing: d})
	}
	return
}

// init registers the disc press here rather than in register.go, whose
// functions the ratchet of long functions holds at their length.
func init() {
	registerAll(allDiscPresses())
	world.RegisterItem(DiscPress{})
}
