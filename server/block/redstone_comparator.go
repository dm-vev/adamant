package block

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// RedstoneComparator is a redstone diode that compares or subtracts signals.
type RedstoneComparator struct {
	carpet
	transparent

	Facing   cube.Direction
	Subtract bool
	Powered  bool
	Output   uint8
}

// BreakInfo ...
func (c RedstoneComparator) BreakInfo() BreakInfo {
	return newBreakInfo(0.5, alwaysHarvestable, nothingEffective, oneOf(c))
}

// UseOnBlock ...
func (c RedstoneComparator) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, c)
	if !used {
		return false
	}
	if !tx.Block(pos.Side(cube.FaceDown)).Model().FaceSolid(pos.Side(cube.FaceDown), cube.FaceUp, tx) {
		return false
	}
	c.Facing = user.Rotation().Direction().Opposite()
	place(tx, pos, c, user, ctx)
	return placed(ctx)
}

// Activate ...
func (c RedstoneComparator) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, _ item.User, _ *item.UseContext) bool {
	c.Subtract = !c.Subtract
	tx.SetBlock(pos, c, nil)
	c.updateOutput(pos, tx)
	return true
}

// NeighbourUpdateTick ...
func (c RedstoneComparator) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !tx.Block(pos.Side(cube.FaceDown)).Model().FaceSolid(pos.Side(cube.FaceDown), cube.FaceUp, tx) {
		breakBlock(c, pos, tx)
		return
	}
	c.updateOutput(pos, tx)
}

// ScheduledTick ...
func (c RedstoneComparator) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	target := c.targetOutput(pos, tx)
	if target == c.Output {
		return
	}
	c.Output = target
	c.Powered = target > 0
	tx.SetBlock(pos, c, nil)
}

// RedstoneWeakPower ...
func (c RedstoneComparator) RedstoneWeakPower(face cube.Face) uint8 {
	if face == c.Facing.Opposite().Face() {
		return c.Output
	}
	return 0
}

// RedstoneStrongPower ...
func (c RedstoneComparator) RedstoneStrongPower(face cube.Face) uint8 {
	return c.RedstoneWeakPower(face)
}

// RedstoneDiodeFacing ...
func (c RedstoneComparator) RedstoneDiodeFacing() cube.Direction {
	return c.Facing
}

// EncodeNBT ...
func (c RedstoneComparator) EncodeNBT() map[string]any {
	return map[string]any{
		"id":           "Comparator",
		"OutputSignal": int32(c.Output),
	}
}

// DecodeNBT ...
func (c RedstoneComparator) DecodeNBT(data map[string]any) any {
	if v, ok := data["OutputSignal"]; ok {
		switch n := v.(type) {
		case int32:
			c.Output = uint8(n)
		case int64:
			c.Output = uint8(n)
		case int:
			c.Output = uint8(n)
		case uint8:
			c.Output = n
		}
	} else {
		c.Output = nbtconv.Uint8(data, "output")
	}
	c.Powered = c.Output > 0
	return c
}

// EncodeItem ...
func (RedstoneComparator) EncodeItem() (name string, meta int16) {
	return "minecraft:comparator", 0
}

// EncodeBlock ...
func (c RedstoneComparator) EncodeBlock() (string, map[string]any) {
	name := "minecraft:unpowered_comparator"
	if c.Powered {
		name = "minecraft:powered_comparator"
	}
	return name, map[string]any{
		"minecraft:cardinal_direction": c.Facing.String(),
		"output_lit_bit":               boolByte(c.Powered),
		"output_subtract_bit":          boolByte(c.Subtract),
	}
}

func (c RedstoneComparator) updateOutput(pos cube.Pos, tx *world.Tx) {
	target := c.targetOutput(pos, tx)
	if target == c.Output {
		return
	}
	tx.ScheduleBlockUpdate(pos, c, redstoneTicks(2))
}

func (c RedstoneComparator) targetOutput(pos cube.Pos, tx *world.Tx) uint8 {
	input := c.inputPower(pos, tx)
	side := c.sidePower(pos, tx)
	if c.Subtract {
		output := input - side
		if output <= 0 {
			return 0
		}
		if output > 15 {
			output = 15
		}
		return uint8(output)
	}
	if input >= side {
		if input > 15 {
			input = 15
		}
		return uint8(input)
	}
	return 0
}

func (c RedstoneComparator) inputPower(pos cube.Pos, tx *world.Tx) int {
	inputFace := c.Facing.Face()
	inputPos := pos.Side(inputFace)
	if output, ok := comparatorOverride(tx, inputPos); ok {
		return int(output)
	}
	power := int(world.RedstonePowerAt(tx, inputPos, inputFace.Opposite()))
	if wire, ok := tx.Block(inputPos).(world.RedstoneWire); ok {
		if wPower := int(wire.RedstoneWirePower()); wPower > power {
			return wPower
		}
	}
	return power
}

func (c RedstoneComparator) sidePower(pos cube.Pos, tx *world.Tx) int {
	return diodeSideInputPower(pos, c.Facing, tx)
}

type comparatorOutputer interface {
	ComparatorOutput(tx *world.Tx, pos cube.Pos) uint8
}

func comparatorOverride(tx *world.Tx, pos cube.Pos) (uint8, bool) {
	b := tx.Block(pos)
	if out, ok := b.(comparatorOutputer); ok {
		return out.ComparatorOutput(tx, pos), true
	}
	if cont, ok := b.(Container); ok {
		return comparatorSignalFromInventory(cont.Inventory(tx, pos)), true
	}
	return 0, false
}

func comparatorSignalFromInventory(inv *inventory.Inventory) uint8 {
	if inv == nil {
		return 0
	}
	slots := inv.Slots()
	if len(slots) == 0 {
		return 0
	}
	filled := 0
	fraction := 0.0
	for _, it := range slots {
		if it.Empty() {
			continue
		}
		filled++
		maxCount := it.MaxCount()
		if maxCount <= 0 {
			maxCount = 1
		}
		fraction += float64(it.Count()) / float64(maxCount)
	}
	if filled == 0 {
		return 0
	}
	fraction /= float64(len(slots))
	signal := int(math.Floor(fraction*14.0)) + 1
	if signal > 15 {
		signal = 15
	}
	return uint8(signal)
}

func allRedstoneComparators() (comparators []world.Block) {
	for _, facing := range cube.Directions() {
		for _, subtract := range []bool{false, true} {
			for _, powered := range []bool{false, true} {
				comparators = append(comparators, RedstoneComparator{Facing: facing, Subtract: subtract, Powered: powered})
			}
		}
	}
	return
}
