package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// RedstoneTorch is a torch that emits redstone power when lit.
type RedstoneTorch struct {
	transparent
	empty

	Facing cube.Face
	Lit    bool
}

// BreakInfo ...
func (t RedstoneTorch) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(t))
}

// LightEmissionLevel ...
func (t RedstoneTorch) LightEmissionLevel() uint8 {
	if !t.Lit {
		return 0
	}
	return 7
}

// UseOnBlock ...
func (t RedstoneTorch) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, face, used := firstReplaceable(tx, pos, face, t)
	if !used {
		return false
	}
	if face == cube.FaceDown {
		return false
	}
	if !tx.Block(pos.Side(face.Opposite())).Model().FaceSolid(pos.Side(face.Opposite()), face, tx) {
		found := false
		for _, i := range []cube.Face{cube.FaceSouth, cube.FaceWest, cube.FaceNorth, cube.FaceEast, cube.FaceDown} {
			if tx.Block(pos.Side(i)).Model().FaceSolid(pos.Side(i), i.Opposite(), tx) {
				found = true
				face = i.Opposite()
				break
			}
		}
		if !found {
			return false
		}
	}
	t.Facing = face.Opposite()
	t.Lit = true
	place(tx, pos, t, user, ctx)
	return placed(ctx)
}

// NeighbourUpdateTick ...
func (t RedstoneTorch) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !tx.Block(pos.Side(t.Facing)).Model().FaceSolid(pos.Side(t.Facing), t.Facing.Opposite(), tx) {
		breakBlock(t, pos, tx)
		return
	}
	tx.ScheduleBlockUpdate(pos, t, redstoneTicks(2))
}

// ScheduledTick ...
func (t RedstoneTorch) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if t.shouldTurnOff(pos, tx) {
		if t.Lit {
			t.Lit = false
			tx.SetBlock(pos, t, nil)
		}
		return
	}
	if !t.Lit {
		t.Lit = true
		tx.SetBlock(pos, t, nil)
	}
}

func (t RedstoneTorch) shouldTurnOff(pos cube.Pos, tx *world.Tx) bool {
	attachedPos := pos.Side(t.Facing)
	attachedFace := t.Facing.Opposite()
	return world.RedstonePowerAt(tx, attachedPos, attachedFace) > 0
}

// RedstoneWeakPower ...
func (t RedstoneTorch) RedstoneWeakPower(face cube.Face) uint8 {
	if !t.Lit {
		return 0
	}
	if face == t.Facing {
		return 0
	}
	return 15
}

// RedstoneStrongPower ...
func (t RedstoneTorch) RedstoneStrongPower(face cube.Face) uint8 {
	if !t.Lit {
		return 0
	}
	if face == cube.FaceDown {
		return 15
	}
	return 0
}

// EncodeItem ...
func (t RedstoneTorch) EncodeItem() (name string, meta int16) {
	return "minecraft:redstone_torch", 0
}

// EncodeBlock ...
func (t RedstoneTorch) EncodeBlock() (name string, properties map[string]any) {
	var face string
	if t.Facing == cube.FaceDown {
		face = "top"
	} else if t.Facing == unknownFace {
		face = "unknown"
	} else {
		face = t.Facing.String()
	}
	if t.Lit {
		return "minecraft:redstone_torch", map[string]any{"torch_facing_direction": face}
	}
	return "minecraft:unlit_redstone_torch", map[string]any{"torch_facing_direction": face}
}

func allRedstoneTorches() (torches []world.Block) {
	for _, face := range cube.Faces() {
		if face == cube.FaceUp {
			face = unknownFace
		}
		torches = append(torches, RedstoneTorch{Facing: face, Lit: true})
		torches = append(torches, RedstoneTorch{Facing: face, Lit: false})
	}
	return
}
