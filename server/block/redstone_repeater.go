package block

import (
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
	tx.Redstone().RefreshCircuit(pos)
	return true
}

// NeighbourUpdateTick ...
func (r RedstoneRepeater) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !tx.Block(pos.Side(cube.FaceDown)).Model().FaceSolid(pos.Side(cube.FaceDown), cube.FaceUp, tx) {
		breakBlock(r, pos, tx)
		return
	}
	tx.Redstone().ScheduleUpdate(pos)
}

func (r RedstoneRepeater) shouldBePowered(pos cube.Pos, tx *world.Tx) bool {
	return r.inputPower(pos, tx) > 0
}

func (r RedstoneRepeater) inputPower(pos cube.Pos, tx *world.Tx) int {
	inputFace := r.Facing.Face()
	return tx.RedstonePowerFrom(pos, inputFace)
}

func (r RedstoneRepeater) isLocked(pos cube.Pos, tx *world.Tx) bool {
	// Bedrock's RepeaterCapacitor accepts only diodes as side-lock sources.
	// A wire can supply a comparator's side input, but cannot lock a repeater.
	for _, face := range []cube.Face{r.Facing.RotateLeft().Face(), r.Facing.RotateRight().Face()} {
		sidePos := pos.Side(face)
		b, loaded := tx.BlockLoaded(sidePos)
		if !loaded {
			continue
		}
		switch b.(type) {
		case RedstoneRepeater, RedstoneComparator:
			if diodeSidePower(sidePos, face, tx) > 0 {
				return true
			}
		}
	}
	return false
}

// RedstonePower returns the repeater output through its front face.
func (r RedstoneRepeater) RedstonePower(_ cube.Pos, _ *world.Tx, face cube.Face) int {
	if !r.Powered {
		return 0
	}
	if face == r.Facing.Opposite().Face() {
		return 15
	}
	return 0
}

// RedstoneStrongPower returns the repeater's directional strong output.
func (r RedstoneRepeater) RedstoneStrongPower(pos cube.Pos, tx *world.Tx, face cube.Face) int {
	return r.RedstonePower(pos, tx, face)
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
