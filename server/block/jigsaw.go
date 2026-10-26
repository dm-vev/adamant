package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

var jigsawHash = NextHash()

// Jigsaw connects the pieces of a jigsaw structure: a piece attaches to the
// jigsaw through a jigsaw of another piece that targets its name. Only
// operators in creative mode can edit it.
type Jigsaw struct {
	solid
	// Facing is the face the jigsaw connects through.
	Facing cube.Face
	// Rotation turns the jigsaw around its facing in quarter turns. Jigsaws
	// facing up or down point their top north, east, south and west, or
	// north, west, south and east for rotations 0 to 3.
	Rotation int
	// Name is the name other jigsaws target to connect to this one.
	Name string
	// Target is the name of the jigsaw this one connects to.
	Target string
	// Pool is the template pool the piece connected to this jigsaw comes from.
	Pool string
	// FinalState is the block state the jigsaw turns into once its structure
	// is generated.
	FinalState string
	// Joint is "rollable" if connected pieces may turn around the jigsaw, or
	// "aligned" if they must keep their top aligned with it.
	Joint string
	// PlacementPriority and SelectionPriority order connecting the jigsaws of
	// a piece.
	PlacementPriority, SelectionPriority int
}

// NewJigsaw returns a jigsaw facing the face passed with the settings of a
// new JigsawBlockEntity.
func NewJigsaw(facing cube.Face, rotation int) Jigsaw {
	joint := "aligned"
	if facing.Axis() == cube.Y {
		joint = "rollable"
	}
	return Jigsaw{Facing: facing, Rotation: rotation, Name: "minecraft:empty", Target: "minecraft:empty", Pool: "minecraft:empty", FinalState: "minecraft:air", Joint: joint}
}

// EncodeItem ...
func (Jigsaw) EncodeItem() (string, int16) { return "minecraft:jigsaw", 0 }

// EncodeBlock ...
func (j Jigsaw) EncodeBlock() (string, map[string]any) {
	return "minecraft:jigsaw", map[string]any{"facing_direction": int32(j.Facing), "rotation": int32(j.Rotation)}
}

// Hash ...
func (j Jigsaw) Hash() (uint64, uint64) {
	return jigsawHash, uint64(j.Facing) | uint64(j.Rotation)<<3
}

// UseOnBlock ports JigsawBlock.getStateForPlacement: the jigsaw faces away
// from the block clicked, with its top up, or facing the player if it points
// up or down.
func (j Jigsaw) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, ok := firstReplaceable(tx, pos, face, j)
	if !ok || user != nil && !canUseGameMasterBlocks(user) {
		// GameMasterBlockItem: only operators in creative mode place it.
		return false
	}
	rotation := 0
	if face.Axis() == cube.Y {
		top := user.Rotation().Direction().Opposite()
		tops := [4]cube.Direction{cube.North, cube.East, cube.South, cube.West}
		if face == cube.FaceDown {
			tops = [4]cube.Direction{cube.North, cube.West, cube.South, cube.East}
		}
		for i, d := range tops {
			if d == top {
				rotation = i
			}
		}
	}
	place(tx, pos, NewJigsaw(face, rotation), user, ctx)
	return placed(ctx)
}

// Activate opens the jigsaw editor for operators in creative mode.
func (Jigsaw) Activate(_ cube.Pos, _ cube.Face, _ *world.Tx, u item.User, _ *item.UseContext) bool {
	return canUseGameMasterBlocks(u)
}

// EncodeNBT encodes the jigsaw with the block entity keys of Bedrock
// Dedicated Server.
func (j Jigsaw) EncodeNBT() map[string]any {
	return map[string]any{
		"id":                 "JigsawBlock",
		"name":               j.Name,
		"target":             j.Target,
		"target_pool":        j.Pool,
		"final_state":        j.FinalState,
		"joint":              j.Joint,
		"placement_priority": int32(j.PlacementPriority),
		"BlockEntityVersion": int32(0),
		"selection_priority": int32(j.SelectionPriority),
	}
}

// DecodeNBT decodes the jigsaw from Bedrock block entity data.
func (j Jigsaw) DecodeNBT(m map[string]any) any {
	d := NewJigsaw(j.Facing, j.Rotation)
	for key, field := range map[string]*string{"name": &d.Name, "target": &d.Target, "target_pool": &d.Pool, "final_state": &d.FinalState} {
		if v, ok := m[key].(string); ok {
			*field = v
		}
	}
	if joint := nbtconv.String(m, "joint"); joint == "rollable" || joint == "aligned" {
		d.Joint = joint
	}
	d.PlacementPriority = int(nbtconv.Int32(m, "placement_priority"))
	d.SelectionPriority = int(nbtconv.Int32(m, "selection_priority"))
	return d
}
