package world

import (
	"fmt"

	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/internal/gamedata"
	"github.com/dm-vev/adamant/server/world/chunk"
)

type chestStater interface {
	WithChestState(Block, cube.Pos, string) Block
}

// restoreJavaBlockActor projects retained source fields once, at actor decode.
// Native-only saves continue to use their own block-actor metadata.
func restoreCanonicalBlockActor(c *chunk.Chunk, pos cube.Pos, b Block) Block {
	id := c.StateID(uint8(pos[0]), int16(pos[1]), uint8(pos[2]))
	if id == 0 {
		return b
	}
	if chest, ok := b.(chestStater); ok {
		b = chest.WithChestState(b, pos, stateString(id, "type"))
	}
	return applyCanonicalActorState(b, id)
}

func applyCanonicalActorState(b Block, id uint32) Block {
	if actor, ok := b.(CanonicalStateBlock); ok {
		name, properties, valid := gamedata.BlockStateFromID(id)
		if !valid {
			panic(fmt.Sprintf("invalid Java actor state ID %d", id))
		}
		updated, err := actor.WithCanonicalState(name, properties)
		if err != nil {
			panic(err)
		}
		return updated
	}
	return b
}

// ResolveStateID projects a canonical Java state through the registry's cached
// native codec. Invalid or unmappable source states panic, as with world writes.
func ResolveStateID(registry chunk.BlockRegistry, id uint32) NativeState {
	if basic, cached := registry.(*BasicBlockRegistry); cached {
		if state, ok := basic.resolvedCanonicalState(id); ok {
			return state
		}
	}
	name, properties, ok := gamedata.BlockStateFromID(id)
	if !ok {
		panic(fmt.Sprintf("invalid Java block state ID %d", id))
	}
	state, err := ResolveState(registry, BlockState{Name: name, Properties: properties})
	if err != nil {
		panic(err)
	}
	return state
}

func (tx *Tx) setCanonicalBlockState(pos cube.Pos, id uint32, flags int) bool {
	if pos.OutOfBounds(tx.Range()) {
		return false
	}
	oldID := tx.canonicalBlockStateID(pos)
	if oldID == id {
		return false
	}
	w := tx.World()
	native := ResolveStateID(w.conf.Blocks, id)
	col := tx.chunk(chunkPosFromBlockPos(pos))
	old := w.blockInChunk(col, pos)
	b := w.conf.Blocks.BlockByRuntimeIDOrAir(native.Block)
	sameBlock := oldID != 0 && stateName(oldID) == stateName(id)
	context := shapeContext{tx: tx, pos: pos}
	keepActor := sameBlock || oldID != 0 && context.tag(oldID, "minecraft:copper_chests") && context.tag(id, "minecraft:copper_chests")
	if chest, ok := b.(chestStater); ok {
		var previous Block
		if keepActor {
			previous = old
		}
		b = chest.WithChestState(previous, pos, stateString(id, "type"))
	} else if keepActor {
		if previous, ok := old.(NBTer); ok {
			if prototype, ok := b.(NBTer); ok {
				if w.conf.Blocks.BlockRuntimeID(old) == native.Block {
					b = old
				} else {
					b = DecodeNBT(prototype, previous.EncodeNBT(), w.conf.Blocks).(Block)
				}
			}
		}
	}
	b = applyCanonicalActorState(b, id)
	_, oldActor := old.(NBTer)
	name := stateName(id)
	liquidBlock := name == "water" || name == "lava"
	tx.setBlock(pos, b, &SetOpts{
		DisableBlockUpdates:       flags&1 == 0,
		DisableLiquidDisplacement: true,
		DisableRedstoneUpdates:    flags&1 == 0,
		nativeState:               &native,
		disableRemovalHandler:     keepActor || flags&256 != 0 && oldActor,
		disablePlacementHandler:   flags&512 != 0 || liquidBlock,
		disableViewUpdates:        flags&2 == 0,
	})
	if flags&512 == 0 && liquidBlock {
		context := shapeContext{tx: tx, pos: pos}
		liquidBlockOnPlace(&context, id)
	}
	return true
}
