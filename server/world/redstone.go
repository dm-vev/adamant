package world

import "github.com/df-mc/dragonfly/server/block/cube"

// RedstonePowerSource exposes redstone power output for a block.
type RedstonePowerSource interface {
	RedstoneWeakPower(face cube.Face) uint8
	RedstoneStrongPower(face cube.Face) uint8
}

// RedstoneWire exposes redstone wire-specific behaviour.
type RedstoneWire interface {
	RedstoneWirePower() uint8
	RedstoneWirePowerTo(pos cube.Pos, face cube.Face, src BlockSource) uint8
	WithRedstoneWirePower(power uint8) Block
}

// RedstoneDiode marks a redstone diode (repeaters/comparators) with a facing direction.
type RedstoneDiode interface {
	RedstoneDiodeFacing() cube.Direction
}

// RedstoneConnectable marks a block that redstone wire can connect to.
type RedstoneConnectable interface {
	RedstoneConnectsTo(face cube.Face) bool
}

// RedstonePowerAt returns the redstone power level emitted from the block at pos towards the neighbour on face.
func RedstonePowerAt(src BlockSource, pos cube.Pos, face cube.Face) uint8 {
	b := src.Block(pos)
	if _, ok := b.(RedstoneWire); ok {
		return redstoneWeakPowerAt(src, pos, face)
	}
	if _, ok := b.(RedstonePowerSource); ok {
		return redstoneWeakPowerAt(src, pos, face)
	}
	if conductor, ok := b.(Conductor); ok && conductor.RedstoneSource() {
		return redstoneWeakPowerAt(src, pos, face)
	}
	if isNormalBlock(src, pos) {
		return redstoneStrongPowerFromNeighbours(src, pos)
	}
	return redstoneWeakPowerAt(src, pos, face)
}

// RedstonePowerFromSide returns the power level emitted from the neighbour on face towards pos.
func RedstonePowerFromSide(src BlockSource, pos cube.Pos, face cube.Face) uint8 {
	return RedstonePowerAt(src, pos.Side(face), face.Opposite())
}

// RedstoneSidePowered returns true if the block adjacent to pos on face emits any power towards pos.
func RedstoneSidePowered(src BlockSource, pos cube.Pos, face cube.Face) bool {
	return RedstonePowerFromSide(src, pos, face) > 0
}

func redstoneWeakPowerAt(src BlockSource, pos cube.Pos, face cube.Face) uint8 {
	b := src.Block(pos)
	if wire, ok := b.(RedstoneWire); ok {
		return wire.RedstoneWirePowerTo(pos, face, src)
	}
	if source, ok := b.(RedstonePowerSource); ok {
		return source.RedstoneWeakPower(face)
	}
	if conductor, ok := b.(Conductor); ok {
		if tx, ok := src.(*Tx); ok {
			return uint8(conductor.WeakPower(pos, face.Opposite(), tx, true))
		}
	}
	return 0
}

func redstoneStrongPowerAt(src BlockSource, pos cube.Pos, face cube.Face) uint8 {
	b := src.Block(pos)
	if wire, ok := b.(RedstoneWire); ok {
		return wire.RedstoneWirePowerTo(pos, face, src)
	}
	if source, ok := b.(RedstonePowerSource); ok {
		return source.RedstoneStrongPower(face)
	}
	if conductor, ok := b.(Conductor); ok {
		if tx, ok := src.(*Tx); ok {
			return uint8(conductor.StrongPower(pos, face.Opposite(), tx, true))
		}
	}
	return 0
}

func redstoneStrongPowerFromNeighbours(src BlockSource, pos cube.Pos) uint8 {
	var power uint8
	for _, face := range cube.Faces() {
		blockPower := redstoneStrongPowerAt(src, pos.Side(face), face.Opposite())
		if blockPower >= 15 {
			return 15
		}
		if blockPower > power {
			power = blockPower
		}
	}
	return power
}

func redstoneStrongPowerFromNeighboursNoWire(src BlockSource, pos cube.Pos) uint8 {
	var power uint8
	for _, face := range cube.Faces() {
		if _, ok := src.Block(pos.Side(face)).(RedstoneWire); ok {
			continue
		}
		blockPower := redstoneStrongPowerAt(src, pos.Side(face), face.Opposite())
		if blockPower >= 15 {
			return 15
		}
		if blockPower > power {
			power = blockPower
		}
	}
	return power
}

func isNormalBlock(src BlockSource, pos cube.Pos) bool {
	b := src.Block(pos)
	for _, face := range cube.Faces() {
		if !b.Model().FaceSolid(pos, face, src) {
			return false
		}
	}
	return true
}
