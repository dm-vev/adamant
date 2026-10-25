package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
)

// SetBlockFromCommand places a block at pos as Java's BlockInput.place and
// Level.updateNeighboursOnBlockSet do for /setblock and /fill. The Java state
// source is first shaped by the blocks around pos, keeping source where that
// would leave air, and the properties source names override the shape. Only
// the neighbours are then updated, so a door half or a torch placed in the air
// stays until a neighbour changes. b is the native block of source and
// carries any block entity data the command gave it.
func (tx *Tx) SetBlockFromCommand(pos cube.Pos, b Block, source BlockState) {
	if pos.OutOfBounds(tx.Range()) {
		return
	}
	registry := tx.World().conf.Blocks
	id, err := gamedata.BlockStateID(NormaliseBlockName(source.Name), source.Properties)
	if err != nil {
		tx.setBlock(pos, b, &SetOpts{DisableBlockUpdates: true})
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
	tx.setBlock(pos, b, &SetOpts{DisableBlockUpdates: true, DisableLiquidDisplacement: true, nativeState: &native})
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
