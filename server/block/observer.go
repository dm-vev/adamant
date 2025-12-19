package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Observer detects block updates and emits a redstone pulse.
type Observer struct {
	solid

	Facing  cube.Face
	Powered bool
}

// BreakInfo ...
func (o Observer) BreakInfo() BreakInfo {
	return newBreakInfo(3, alwaysHarvestable, pickaxeEffective, oneOf(o))
}

// UseOnBlock ...
func (o Observer) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, o)
	if !used {
		return false
	}
	o.Facing = calculateFace(user, pos)
	o.Powered = false
	place(tx, pos, o, user, ctx)
	return placed(ctx)
}

// NeighbourUpdateTick ...
func (o Observer) NeighbourUpdateTick(pos, changedNeighbour cube.Pos, tx *world.Tx) {
	if changedNeighbour != pos.Side(o.Facing) {
		return
	}
	if o.Powered {
		return
	}
	o.Powered = true
	tx.SetBlock(pos, o, nil)
	tx.ScheduleBlockUpdate(pos, o, redstoneTicks(2))
}

// ScheduledTick ...
func (o Observer) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if !o.Powered {
		return
	}
	o.Powered = false
	tx.SetBlock(pos, o, nil)
}

// RedstoneWeakPower ...
func (o Observer) RedstoneWeakPower(face cube.Face) uint8 {
	if o.Powered && face == o.Facing.Opposite() {
		return 15
	}
	return 0
}

// RedstoneStrongPower ...
func (o Observer) RedstoneStrongPower(face cube.Face) uint8 {
	return o.RedstoneWeakPower(face)
}

// EncodeItem ...
func (Observer) EncodeItem() (name string, meta int16) {
	return "minecraft:observer", 0
}

// EncodeBlock ...
func (o Observer) EncodeBlock() (string, map[string]any) {
	return "minecraft:observer", map[string]any{
		"minecraft:facing_direction": o.Facing.String(),
		"powered_bit":                boolByte(o.Powered),
	}
}

func allObservers() (observers []world.Block) {
	for _, face := range cube.Faces() {
		for _, powered := range []bool{false, true} {
			observers = append(observers, Observer{Facing: face, Powered: powered})
		}
	}
	return
}
