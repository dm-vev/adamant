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

	// Powered selects the powered/unpowered block identifier. Lit is the
	// independent output_lit_bit; commands may leave the two out of sync.
	Powered bool
	Lit     bool
	Output  uint8
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
	tx.Redstone().RefreshCircuit(pos)
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

// ScheduledTick reconciles legacy saved callbacks. Evaluation runs on the
// world's shared circuit clock rather than starting a private input timer.
func (RedstoneComparator) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if tx != nil {
		tx.Redstone().ScheduleUpdate(pos)
	}
}

// RedstonePower returns the comparator output through its front face.
func (c RedstoneComparator) RedstonePower(pos cube.Pos, tx *world.Tx, face cube.Face) int {
	if face == c.circuitFacing(pos, tx).Opposite() {
		return int(c.Output)
	}
	return 0
}

func (c RedstoneComparator) circuitFacing(pos cube.Pos, tx *world.Tx) cube.Face {
	if tx != nil {
		if facing, ok := tx.Redstone().CircuitDirection(pos); ok {
			return facing
		}
	}
	return c.Facing.Face()
}

// RedstoneStrongPower returns the comparator's directional strong output.
func (c RedstoneComparator) RedstoneStrongPower(pos cube.Pos, tx *world.Tx, face cube.Face) int {
	return c.RedstonePower(pos, tx, face)
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
		"output_lit_bit":               boolByte(c.Lit),
		"output_subtract_bit":          boolByte(c.Subtract),
	}
}

func (RedstoneComparator) updateOutput(pos cube.Pos, tx *world.Tx) {
	tx.Redstone().ScheduleUpdate(pos)
}

// comparatorTargetOutput follows ComparatorCapacitor::evaluate. Zero crossings
// of this strength control lit transitions, including subtract/equal output zero.
func comparatorTargetOutput(input, side int, subtract bool) uint8 {
	if subtract {
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

func (c RedstoneComparator) analogInputPower(pos cube.Pos, tx *world.Tx) (uint8, bool) {
	inputFace := c.Facing.Face()
	inputPos := pos.Side(inputFace)
	if output, ok := comparatorOverride(tx, inputPos, inputFace.Opposite()); ok {
		return output, true
	}
	// Bedrock's block actor installs a readable analog rear override even
	// when the intervening conductor has a direct strength of 15.
	if b, loaded := tx.BlockLoaded(inputPos); loaded && world.RedstoneFullPowerConductor(inputPos, b, tx) {
		if output, ok := comparatorOverride(tx, inputPos.Side(inputFace), inputFace.Opposite()); ok {
			return output, true
		}
	}
	return 0, false
}

type comparatorOutputer interface {
	ComparatorOutput(tx *world.Tx, pos cube.Pos) uint8
}

// directionalComparatorOutputer is a block whose comparator output depends on
// the side the comparator reads it from.
type directionalComparatorOutputer interface {
	// ComparatorOutputTowards returns the output read by a comparator on the
	// face towards of the block.
	ComparatorOutputTowards(tx *world.Tx, pos cube.Pos, towards cube.Face) uint8
}

// comparatorOverride returns the output a comparator on the face towards of
// the block at pos reads from it, if the block is loaded and has one.
func comparatorOverride(tx *world.Tx, pos cube.Pos, towards cube.Face) (uint8, bool) {
	b, loaded := tx.BlockLoaded(pos)
	if !loaded {
		return 0, false
	}
	if out, ok := b.(directionalComparatorOutputer); ok {
		return out.ComparatorOutputTowards(tx, pos, towards), true
	}
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
	// AbstractContainerMenu.getRedstoneSignalFromContainer, in float.
	var fraction float32
	for _, it := range slots {
		if it.Empty() {
			continue
		}
		fraction += float32(it.Count()) / float32(max(it.MaxCount(), 1))
	}
	fraction /= float32(len(slots))
	// Mth.lerpDiscrete(fraction, 0, 15).
	signal := int(math.Floor(float64(fraction * 14)))
	if fraction > 0 {
		signal++
	}
	return uint8(min(signal, 15))
}

func allRedstoneComparators() (comparators []world.Block) {
	for _, facing := range cube.Directions() {
		for _, subtract := range []bool{false, true} {
			for _, powered := range []bool{false, true} {
				for _, lit := range []bool{false, true} {
					comparators = append(comparators, RedstoneComparator{Facing: facing, Subtract: subtract, Powered: powered, Lit: lit})
				}
			}
		}
	}
	return
}
