package world

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
)

// CanFillBlock checks whether a block accepts a command fill at pos.
func (tx *Tx) CanFillBlock(pos cube.Pos, b Block) bool {
	if pos.OutOfBounds(tx.Range()) {
		return false
	}
	if validator, ok := b.(BlockFillValidator); ok {
		return validator.CanFillAtPos(pos, tx)
	}
	return true
}

// SetBlockFromCommand supplies b unchanged to its native fill lifecycle when
// implemented. Other blocks use Java's BlockInput.place: source is shaped by
// neighbours, preserving it if shaping would leave air, then its explicitly
// supplied properties override the shape. Only neighbours are notified. b
// carries any block entity data supplied by the command.
func (tx *Tx) SetBlockFromCommand(pos cube.Pos, b Block, source BlockState) {
	tx.setBlockFromCommand(pos, b, source, true)
}

// SetBlockFromFillCommand places a cell, deferring native neighbour publication
// until FinishBlockFill after the batch has installed every requested cell.
func (tx *Tx) SetBlockFromFillCommand(pos cube.Pos, b Block, source BlockState) {
	tx.setBlockFromCommand(pos, b, source, false)
}

// FinishBlockFill runs the native post-fill notifications and tick executors.
func (tx *Tx) FinishBlockFill(positions []cube.Pos) {
	for _, pos := range positions {
		b := tx.Block(pos)
		if _, nativeFill := b.(BlockFillHandler); !nativeFill {
			continue
		}
		tx.updateNeighboursOf(pos)
		if ticker, ok := b.(BlockFillTicker); ok && ticker.TickAfterCommandFill() {
			tx.ScheduleBlockUpdate(pos, b, time.Second/20)
		}
	}
}

func (tx *Tx) setBlockFromCommand(pos cube.Pos, b Block, source BlockState, notifyNative bool) {
	if !tx.CanFillBlock(pos, b) {
		return
	}
	if _, nativeFill := b.(BlockFillHandler); nativeFill {
		// Native fill callbacks receive the requested permutation unchanged.
		// Java neighbour shaping can borrow a different door's material.
		tx.setBlock(pos, b, &SetOpts{DisableBlockUpdates: true, CommandFill: true})
		if notifyNative {
			tx.updateNeighboursOf(pos)
		}
		return
	}
	registry := tx.World().conf.Blocks
	id, err := gamedata.BlockStateID(NormaliseBlockName(source.Name), source.Properties)
	if err != nil {
		tx.setBlock(pos, b, &SetOpts{DisableBlockUpdates: true, CommandFill: true})
		tx.updateNeighboursOf(pos)
		return
	}
	shaped := tx.javaShapeFromNeighbours(pos, id)
	for property, value := range source.Properties {
		if defined, err := gamedata.BlockStateWithProperty(shaped, property, value); err == nil {
			shaped = defined
		}
	}
	native := ResolveStateID(registry, shaped)
	if native.Block != registry.BlockRuntimeID(b) {
		shapedBlock := registry.BlockByRuntimeIDOrAir(native.Block)
		if prototype, ok := shapedBlock.(NBTer); ok {
			data := map[string]any{}
			if given, ok := b.(NBTer); ok {
				data = given.EncodeNBT()
			}
			shapedBlock = DecodeNBT(prototype, data, registry).(Block)
		}
		b = shapedBlock
	}
	tx.setBlock(pos, b, &SetOpts{DisableBlockUpdates: true, DisableLiquidDisplacement: true, CommandFill: true, nativeState: &native})
	tx.updateNeighboursOf(pos)
}

// SetBlockKnownShape places b at pos as Java's /clone does, with the block's
// shape already known: b is not updated itself, only its neighbours are.
func (tx *Tx) SetBlockKnownShape(pos cube.Pos, b Block) {
	if pos.OutOfBounds(tx.Range()) {
		return
	}
	tx.setBlock(pos, b, &SetOpts{DisableBlockUpdates: true})
	tx.updateNeighboursOf(pos)
}

// javaShapeFromNeighbours ports Block.updateFromNeighbourShapes as
// BlockInput.place uses it: state is returned unchanged if shaping it would
// leave air, or if a neighbour has no Java state to shape it by.
func (tx *Tx) javaShapeFromNeighbours(pos cube.Pos, state uint32) uint32 {
	for _, face := range cube.Faces() {
		if _, ok := gamedata.BlockStateName(tx.javaBlockStateID(pos.Side(face))); !ok {
			return state
		}
	}
	context := javaShapeContext{tx: tx, pos: pos}
	if shaped := context.updateFromNeighbourShapes(state); !javaIsAir(shaped) {
		return shaped
	}
	return state
}

// updateNeighboursOf queues neighbour updates for the blocks around pos, but
// not for pos itself, as Level.updateNeighborsAt does.
func (tx *Tx) updateNeighboursOf(pos cube.Pos) {
	w := tx.World()
	pos.Neighbours(func(neighbour cube.Pos) {
		tx.updateNeighbour(neighbour, pos)
	}, w.Range())
}
