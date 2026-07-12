package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TripwireHook is a redstone component that detects a tripwire line.
type TripwireHook struct {
	empty
	transparent

	Facing   cube.Direction
	Attached bool
	Powered  bool
}

// BreakInfo ...
func (t TripwireHook) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(t))
}

// UseOnBlock ...
func (t TripwireHook) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, face, used := firstReplaceable(tx, pos, face, t)
	if !used {
		return false
	}
	if face.Axis() == cube.Y {
		return false
	}
	if !tx.Block(pos.Side(face.Opposite())).Model().FaceSolid(pos.Side(face.Opposite()), face, tx) {
		return false
	}
	t.Facing = face.Direction()
	place(tx, pos, t, user, ctx)
	updateTripwireLine(pos, t, tx)
	return placed(ctx)
}

// NeighbourUpdateTick ...
func (t TripwireHook) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	attachedPos := pos.Side(t.Facing.Opposite().Face())
	if !tx.Block(attachedPos).Model().FaceSolid(attachedPos, t.Facing.Face(), tx) {
		breakBlock(t, pos, tx)
		return
	}
	updateTripwireLine(pos, t, tx)
}

func (t TripwireHook) RedstonePower(cube.Pos, *world.Tx, cube.Face) int {
	if t.Powered {
		return 15
	}
	return 0
}

func (t TripwireHook) RedstoneStrongPower(_ cube.Pos, _ *world.Tx, face cube.Face) int {
	if !t.Powered {
		return 0
	}
	if face == t.Facing.Opposite().Face() {
		return 15
	}
	return 0
}

// EncodeItem ...
func (TripwireHook) EncodeItem() (name string, meta int16) {
	return "minecraft:tripwire_hook", 0
}

// EncodeBlock ...
func (t TripwireHook) EncodeBlock() (string, map[string]any) {
	return "minecraft:tripwire_hook", map[string]any{
		"attached_bit": boolByte(t.Attached),
		"direction":    int32(horizontalDirection(t.Facing)),
		"powered_bit":  boolByte(t.Powered),
	}
}

func allTripwireHooks() (hooks []world.Block) {
	for _, facing := range cube.Directions() {
		for _, attached := range []bool{false, true} {
			for _, powered := range []bool{false, true} {
				hooks = append(hooks, TripwireHook{Facing: facing, Attached: attached, Powered: powered})
			}
		}
	}
	return
}
