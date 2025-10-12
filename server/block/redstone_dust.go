package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// RedstoneDust represents redstone wire laid on the ground.
type RedstoneDust struct {
	transparent
	empty

	// Power is the current signal strength carried by the dust (0-15).
	Power uint8
}

// EncodeItem ...
func (RedstoneDust) EncodeItem() (name string, meta int16) {
	return "minecraft:redstone", 0
}

// EncodeBlock ...
func (d RedstoneDust) EncodeBlock() (string, map[string]any) {
	props := map[string]any{
		"redstone_signal": int32(d.Power),
		"north":           "none",
		"south":           "none",
		"east":            "none",
		"west":            "none",
	}
	return "minecraft:redstone_wire", props
}

// UseOnBlock places the dust on top of a solid block.
func (d RedstoneDust) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, face, used := firstReplaceable(tx, pos, face, d)
	if !used {
		return false
	}
	if face == cube.FaceDown {
		return false
	}
	support := pos.Side(cube.FaceDown)
	if !tx.Block(support).Model().FaceSolid(support, cube.FaceUp, tx) {
		return false
	}
	d.Power = 0
	place(tx, pos, d, user, ctx)
	return placed(ctx)
}

// NeighbourUpdateTick removes the dust if no longer supported.
func (d RedstoneDust) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	support := pos.Side(cube.FaceDown)
	if !tx.Block(support).Model().FaceSolid(support, cube.FaceUp, tx) {
		breakBlock(d, pos, tx)
	}
}

// Model ...
func (RedstoneDust) Model() world.BlockModel {
	return model.RedstoneDust{}
}

// allRedstoneDust returns all dust block states for registration.
func allRedstoneDust() []world.Block {
	blocks := make([]world.Block, 0, 16)
	for p := 0; p < 16; p++ {
		blocks = append(blocks, RedstoneDust{Power: uint8(p)})
	}
	return blocks
}

// Hash implements world.Block. Returning zeros forces slow-path by name/properties.
func (RedstoneDust) Hash() (uint64, uint64) { return 0, 0 }
