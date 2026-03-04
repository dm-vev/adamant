package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// RedstoneWire is a thin redstone component that transmits power.
type RedstoneWire struct {
	carpet
	replaceable
	transparent
	sourceWaterDisplacer

	Power int
}

// BreakInfo ...
func (r RedstoneWire) BreakInfo() BreakInfo {
	return newBreakInfo(0.1, alwaysHarvestable, nothingEffective, oneOf(r))
}

// SideClosed ...
func (RedstoneWire) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// HasLiquidDrops ...
func (RedstoneWire) HasLiquidDrops() bool {
	return true
}

// NeighbourUpdateTick ...
func (r RedstoneWire) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !tx.Block(pos.Side(cube.FaceDown)).Model().FaceSolid(pos.Side(cube.FaceDown), cube.FaceUp, tx) {
		breakBlock(r, pos, tx)
		return
	}
	r.calculateCurrentChanges(pos, tx, false, true)
}

// UseOnBlock ...
func (r RedstoneWire) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, r)
	if !used {
		return false
	}
	if !tx.Block(pos.Side(cube.FaceDown)).Model().FaceSolid(pos.Side(cube.FaceDown), cube.FaceUp, tx) {
		return false
	}
	r.Power = 0
	place(tx, pos, r, user, ctx)
	r.calculateCurrentChanges(pos, tx, true, true)

	for _, vertical := range []cube.Face{cube.FaceDown, cube.FaceUp} {
		r.updateAround(pos.Side(vertical), tx)
	}

	for _, side := range cube.HorizontalFaces() {
		sidePos := pos.Side(side)
		if wireIsNormalBlock(sidePos, tx) {
			r.updateAround(sidePos.Side(cube.FaceUp), tx)
			continue
		}
		r.updateAround(sidePos.Side(cube.FaceDown), tx)
	}
	return placed(ctx)
}

func (r RedstoneWire) updateAround(pos cube.Pos, tx *world.Tx) {
	if _, ok := tx.Block(pos).(world.RedstoneWire); !ok {
		return
	}
	tx.DoBlockUpdatesAround(pos)
	for _, side := range cube.Faces() {
		tx.DoBlockUpdatesAround(pos.Side(side))
	}
}

func (r RedstoneWire) calculateCurrentChanges(pos cube.Pos, tx *world.Tx, force, stillExists bool) {
	meta := r.Power
	maxStrength := meta
	power := wireIndirectPower(pos, tx)
	if power > 0 && power > maxStrength-1 {
		maxStrength = power
	}

	strength := 0
	for _, side := range cube.HorizontalFaces() {
		sidePos := pos.Side(side)
		strength = wireMaxCurrentStrength(sidePos, strength, tx)

		sideNormal := wireIsNormalBlock(sidePos, tx)
		if sideNormal && !wireIsNormalBlock(pos.Side(cube.FaceUp), tx) {
			strength = wireMaxCurrentStrength(sidePos.Side(cube.FaceUp), strength, tx)
		} else if !sideNormal {
			strength = wireMaxCurrentStrength(sidePos.Side(cube.FaceDown), strength, tx)
		}
	}

	if strength > maxStrength {
		maxStrength = strength - 1
	} else if maxStrength > 0 {
		maxStrength--
	} else {
		maxStrength = 0
	}

	if power > maxStrength-1 {
		maxStrength = power
	} else if power < maxStrength && strength <= maxStrength {
		maxStrength = maxInt(power, strength-1)
	}

	if maxStrength < 0 {
		maxStrength = 0
	} else if maxStrength > 15 {
		maxStrength = 15
	}

	if meta != maxStrength {
		if stillExists {
			r.Power = maxStrength
			tx.SetBlock(pos, r, &world.SetOpts{DisableBlockUpdates: true})
		}
		tx.DoBlockUpdatesAround(pos)
		for _, side := range cube.Faces() {
			tx.DoBlockUpdatesAround(pos.Side(side))
		}
		return
	}
	if !force {
		return
	}
	for _, side := range cube.Faces() {
		tx.DoBlockUpdatesAround(pos.Side(side))
	}
}

func wireMaxCurrentStrength(pos cube.Pos, maxStrength int, src world.BlockSource) int {
	wire, ok := src.Block(pos).(world.RedstoneWire)
	if !ok {
		return maxStrength
	}
	strength := int(wire.RedstoneWirePower())
	if strength > maxStrength {
		return strength
	}
	return maxStrength
}

func wireIndirectPower(pos cube.Pos, src world.BlockSource) int {
	power := 0
	for _, face := range cube.Faces() {
		blockPower := wireIndirectPowerFrom(pos.Side(face), face, src)
		if blockPower >= 15 {
			return 15
		}
		if blockPower > power {
			power = blockPower
		}
	}
	return power
}

func wireIndirectPowerFrom(pos cube.Pos, face cube.Face, src world.BlockSource) int {
	if _, ok := src.Block(pos).(world.RedstoneWire); ok {
		return 0
	}
	if wireIsNormalBlock(pos, src) {
		return int(wireStrongPowerFromNeighboursNoWire(src, pos))
	}
	return int(wireWeakPowerAt(src, pos, face.Opposite()))
}

func wireWeakPowerAt(src world.BlockSource, pos cube.Pos, face cube.Face) uint8 {
	if wire, ok := src.Block(pos).(world.RedstoneWire); ok {
		return wire.RedstoneWirePowerTo(pos, face, src)
	}
	if source, ok := src.Block(pos).(world.RedstonePowerSource); ok {
		return source.RedstoneWeakPower(face)
	}
	return 0
}

func wireStrongPowerAt(src world.BlockSource, pos cube.Pos, face cube.Face) uint8 {
	if wire, ok := src.Block(pos).(world.RedstoneWire); ok {
		return wire.RedstoneWirePowerTo(pos, face, src)
	}
	if source, ok := src.Block(pos).(world.RedstonePowerSource); ok {
		return source.RedstoneStrongPower(face)
	}
	return 0
}

func wireStrongPowerFromNeighboursNoWire(src world.BlockSource, pos cube.Pos) uint8 {
	var power uint8
	for _, face := range cube.Faces() {
		if _, ok := src.Block(pos.Side(face)).(world.RedstoneWire); ok {
			continue
		}
		blockPower := wireStrongPowerAt(src, pos.Side(face), face.Opposite())
		if blockPower >= 15 {
			return 15
		}
		if blockPower > power {
			power = blockPower
		}
	}
	return power
}

// EncodeItem ...
func (r RedstoneWire) EncodeItem() (name string, meta int16) {
	return "minecraft:redstone", 0
}

// EncodeBlock ...
func (r RedstoneWire) EncodeBlock() (name string, properties map[string]any) {
	return "minecraft:redstone_wire", map[string]any{"redstone_signal": int32(r.Power)}
}

// RedstoneWirePower ...
func (r RedstoneWire) RedstoneWirePower() uint8 {
	return uint8(r.Power)
}

// WithRedstoneWirePower ...
func (r RedstoneWire) WithRedstoneWirePower(power uint8) world.Block {
	r.Power = int(power)
	return r
}

// RedstoneWirePowerTo returns the power emitted towards a face.
func (r RedstoneWire) RedstoneWirePowerTo(pos cube.Pos, face cube.Face, src world.BlockSource) uint8 {
	if r.Power == 0 {
		return 0
	}
	if face == cube.FaceUp {
		return uint8(r.Power)
	}
	if face.Axis() == cube.Y {
		return 0
	}

	connections := map[cube.Face]struct{}{}
	for _, side := range cube.HorizontalFaces() {
		if r.isPowerSourceAt(pos, side, src) {
			connections[side] = struct{}{}
		}
	}

	if len(connections) == 0 {
		return uint8(r.Power)
	}
	if _, ok := connections[face]; ok {
		if _, ok := connections[face.RotateLeft()]; ok {
			return 0
		}
		if _, ok := connections[face.RotateRight()]; ok {
			return 0
		}
		return uint8(r.Power)
	}
	return 0
}

func (r RedstoneWire) isPowerSourceAt(pos cube.Pos, side cube.Face, src world.BlockSource) bool {
	sidePos := pos.Side(side)
	block := src.Block(sidePos)
	sideNormal := wireIsNormalBlock(sidePos, src)
	if !wireIsNormalBlock(pos.Side(cube.FaceUp), src) && sideNormal && canConnectUpwardsTo(src.Block(sidePos.Side(cube.FaceUp))) {
		return true
	}
	if canConnectTo(block, side) {
		return true
	}
	if !sideNormal && canConnectUpwardsTo(src.Block(sidePos.Side(cube.FaceDown))) {
		return true
	}
	return false
}

func canConnectUpwardsTo(b world.Block) bool {
	if _, ok := b.(world.RedstoneWire); ok {
		return true
	}
	return false
}

func canConnectTo(b world.Block, side cube.Face) bool {
	if _, ok := b.(world.RedstoneWire); ok {
		return true
	}
	if diode, ok := b.(world.RedstoneDiode); ok {
		facing := diode.RedstoneDiodeFacing().Face()
		return facing == side || facing.Opposite() == side
	}
	if _, ok := b.(world.RedstonePowerSource); ok {
		return true
	}
	return false
}

func wireIsNormalBlock(pos cube.Pos, src world.BlockSource) bool {
	return redstoneNormalBlock(pos, src)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func allRedstoneWires() (wires []world.Block) {
	for i := 0; i <= 15; i++ {
		wires = append(wires, RedstoneWire{Power: i})
	}
	return
}
