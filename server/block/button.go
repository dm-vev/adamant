package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Button is a redstone component that emits a short pulse.
type Button struct {
	transparent
	empty

	Type    ButtonType
	Facing  cube.Face
	Pressed bool
}

// BreakInfo ...
func (b Button) BreakInfo() BreakInfo {
	return newBreakInfo(0.5, alwaysHarvestable, nothingEffective, oneOf(b))
}

// UseOnBlock ...
func (b Button) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, face, used := firstReplaceable(tx, pos, face, b)
	if !used {
		return false
	}
	if !tx.Block(pos.Side(face.Opposite())).Model().FaceSolid(pos.Side(face.Opposite()), face, tx) {
		return false
	}
	b.Facing = face
	place(tx, pos, b, user, ctx)
	return placed(ctx)
}

// Activate ...
func (b Button) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, _ item.User, _ *item.UseContext) bool {
	if b.Pressed {
		return false
	}
	b.Pressed = true
	tx.SetBlock(pos, b, nil)
	tx.ScheduleBlockUpdate(pos, b, redstoneTicks(b.Type.pressTicks()))
	tx.DoBlockUpdatesAround(pos)
	tx.DoBlockUpdatesAround(pos.Side(b.Facing.Opposite()))
	return true
}

// NeighbourUpdateTick ...
func (b Button) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !tx.Block(pos.Side(b.Facing.Opposite())).Model().FaceSolid(pos.Side(b.Facing.Opposite()), b.Facing, tx) {
		breakBlock(b, pos, tx)
	}
}

// ScheduledTick ...
func (b Button) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if !b.Pressed {
		return
	}
	b.Pressed = false
	tx.SetBlock(pos, b, nil)
	tx.DoBlockUpdatesAround(pos)
	tx.DoBlockUpdatesAround(pos.Side(b.Facing.Opposite()))
}

// RedstoneWeakPower ...
func (b Button) RedstoneWeakPower(cube.Face) uint8 {
	if b.Pressed {
		return 15
	}
	return 0
}

// RedstoneStrongPower ...
func (b Button) RedstoneStrongPower(face cube.Face) uint8 {
	if !b.Pressed {
		return 0
	}
	if face == b.Facing.Opposite() {
		return 15
	}
	return 0
}

// EncodeItem ...
func (b Button) EncodeItem() (name string, meta int16) {
	return "minecraft:" + b.Type.blockName(), 0
}

// EncodeBlock ...
func (b Button) EncodeBlock() (string, map[string]any) {
	return "minecraft:" + b.Type.blockName(), map[string]any{"button_pressed_bit": boolByte(b.Pressed), "facing_direction": int32(b.Facing)}
}

func allButtons() (buttons []world.Block) {
	for _, t := range ButtonTypes() {
		for _, face := range cube.Faces() {
			buttons = append(buttons, Button{Type: t, Facing: face, Pressed: false})
			buttons = append(buttons, Button{Type: t, Facing: face, Pressed: true})
		}
	}
	return
}
