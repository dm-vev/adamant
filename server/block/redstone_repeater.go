package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// RedstoneRepeater is a redstone diode that delays a signal.
type RedstoneRepeater struct {
	carpet
	transparent

	Facing  cube.Direction
	Delay   int
	Powered bool
}

// BreakInfo ...
func (r RedstoneRepeater) BreakInfo() BreakInfo {
	return newBreakInfo(0.5, alwaysHarvestable, nothingEffective, oneOf(r))
}

// UseOnBlock ...
func (r RedstoneRepeater) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, r)
	if !used {
		return false
	}
	if !tx.Block(pos.Side(cube.FaceDown)).Model().FaceSolid(pos.Side(cube.FaceDown), cube.FaceUp, tx) {
		return false
	}
	r.Facing = user.Rotation().Direction().Opposite()
	place(tx, pos, r, user, ctx)
	return placed(ctx)
}

// Activate cycles the delay.
func (r RedstoneRepeater) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, _ item.User, _ *item.UseContext) bool {
	r.Delay = (r.Delay + 1) % 4
	tx.SetBlock(pos, r, nil)
	return true
}

// NeighbourUpdateTick ...
func (r RedstoneRepeater) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !tx.Block(pos.Side(cube.FaceDown)).Model().FaceSolid(pos.Side(cube.FaceDown), cube.FaceUp, tx) {
		breakBlock(r, pos, tx)
		return
	}
	r.updateState(pos, tx)
}

// ScheduledTick ...
func (r RedstoneRepeater) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if r.isLocked(pos, tx) {
		return
	}
	outputPos := pos.Side(r.Facing.Opposite().Face())
	shouldPower := r.shouldBePowered(pos, tx)
	if r.Powered && !shouldPower {
		r.Powered = false
		tx.SetBlock(pos, r, &world.SetOpts{DisableBlockUpdates: true})
		tx.DoBlockUpdatesAround(outputPos)
		return
	}
	if !r.Powered && shouldPower {
		r.Powered = true
		tx.SetBlock(pos, r, &world.SetOpts{DisableBlockUpdates: true})
		tx.DoBlockUpdatesAround(outputPos)
		if !r.shouldBePowered(pos, tx) {
			tx.ScheduleBlockUpdate(pos, r, redstoneTicks(r.delayTicks()))
		}
	}
}

func (r RedstoneRepeater) updateState(pos cube.Pos, tx *world.Tx) {
	if r.isLocked(pos, tx) {
		return
	}
	shouldPower := r.shouldBePowered(pos, tx)
	if r.Powered == shouldPower {
		return
	}
	tx.ScheduleBlockUpdate(pos, r, redstoneTicks(r.delayTicks()))
}

func (r RedstoneRepeater) shouldBePowered(pos cube.Pos, tx *world.Tx) bool {
	return r.inputPower(pos, tx) > 0
}

func (r RedstoneRepeater) inputPower(pos cube.Pos, tx *world.Tx) int {
	inputFace := r.Facing.Face()
	inputPos := pos.Side(inputFace)
	power := int(world.RedstonePowerAt(tx, inputPos, inputFace.Opposite()))
	if power >= 15 {
		return power
	}
	if wire, ok := tx.Block(inputPos).(world.RedstoneWire); ok {
		if wPower := int(wire.RedstoneWirePower()); wPower > power {
			return wPower
		}
	}
	return power
}

func (r RedstoneRepeater) isLocked(pos cube.Pos, tx *world.Tx) bool {
	return diodeSideInputPower(pos, r.Facing, tx) > 0
}

func (r RedstoneRepeater) delayTicks() int {
	return (1 + r.Delay) * 2
}

// RedstoneWeakPower ...
func (r RedstoneRepeater) RedstoneWeakPower(face cube.Face) uint8 {
	if !r.Powered {
		return 0
	}
	if face == r.Facing.Opposite().Face() {
		return 15
	}
	return 0
}

// RedstoneStrongPower ...
func (r RedstoneRepeater) RedstoneStrongPower(face cube.Face) uint8 {
	return r.RedstoneWeakPower(face)
}

// RedstoneDiodeFacing ...
func (r RedstoneRepeater) RedstoneDiodeFacing() cube.Direction {
	return r.Facing
}

// EncodeItem ...
func (r RedstoneRepeater) EncodeItem() (name string, meta int16) {
	return "minecraft:repeater", 0
}

// EncodeBlock ...
func (r RedstoneRepeater) EncodeBlock() (string, map[string]any) {
	if r.Powered {
		return "minecraft:powered_repeater", map[string]any{"minecraft:cardinal_direction": r.Facing.String(), "repeater_delay": int32(r.Delay)}
	}
	return "minecraft:unpowered_repeater", map[string]any{"minecraft:cardinal_direction": r.Facing.String(), "repeater_delay": int32(r.Delay)}
}

func allRepeaters() (repeaters []world.Block) {
	for _, facing := range cube.Directions() {
		for delay := 0; delay < 4; delay++ {
			repeaters = append(repeaters, RedstoneRepeater{Facing: facing, Delay: delay, Powered: false})
			repeaters = append(repeaters, RedstoneRepeater{Facing: facing, Delay: delay, Powered: true})
		}
	}
	return
}
