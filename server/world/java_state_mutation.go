package world

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type javaChestState interface {
	WithJavaChestState(Block, cube.Pos, string) Block
}

// restoreJavaBlockActor projects retained source fields once, at actor decode.
// Native-only saves continue to use their own block-actor metadata.
func restoreJavaBlockActor(c *chunk.Chunk, pos cube.Pos, b Block) Block {
	id := c.JavaStateID(uint8(pos[0]), int16(pos[1]), uint8(pos[2]))
	if id == 0 {
		return b
	}
	if chest, ok := b.(javaChestState); ok {
		b = chest.WithJavaChestState(b, pos, javaString(id, "type"))
	}
	return applyJavaActorState(b, id)
}

func applyJavaActorState(b Block, id uint32) Block {
	if actor, ok := b.(JavaStateBlock); ok {
		name, properties, valid := javadata.BlockStateFromID(id)
		if !valid {
			panic(fmt.Sprintf("invalid Java actor state ID %d", id))
		}
		updated, err := actor.WithJavaBlockState(name, properties)
		if err != nil {
			panic(err)
		}
		return updated
	}
	return b
}

// resolveJavaStateID caches the forward projection independently of the native
// inverse: multiple source states may share the same native layer pair.
func (w *World) resolveJavaStateID(id uint32) NativeState {
	registry := w.conf.Blocks
	basic, cached := registry.(*BasicBlockRegistry)
	if cached {
		if state, ok := basic.javaNativeStates.Load(id); ok {
			return state.(NativeState)
		}
	}
	name, properties, ok := javadata.BlockStateFromID(id)
	if !ok {
		panic(fmt.Sprintf("invalid Java block state ID %d", id))
	}
	state, err := ResolveState(registry, BlockState{Name: name, Properties: properties})
	if err != nil {
		panic(err)
	}
	if cached {
		basic.javaNativeStates.Store(id, state)
	}
	return state
}

func (tx *Tx) setJavaBlockState(pos cube.Pos, id uint32, flags int) bool {
	if pos.OutOfBounds(tx.Range()) {
		return false
	}
	oldID := tx.javaBlockStateID(pos)
	if oldID == id {
		return false
	}
	w := tx.World()
	native := w.resolveJavaStateID(id)
	col := tx.chunk(chunkPosFromBlockPos(pos))
	old := w.blockInChunk(col, pos)
	b := w.conf.Blocks.BlockByRuntimeIDOrAir(native.Block)
	sameBlock := oldID != 0 && javaName(oldID) == javaName(id)
	context := javaShapeContext{tx: tx, pos: pos}
	keepActor := sameBlock || oldID != 0 && context.tag(oldID, "minecraft:copper_chests") && context.tag(id, "minecraft:copper_chests")
	if chest, ok := b.(javaChestState); ok {
		var previous Block
		if keepActor {
			previous = old
		}
		b = chest.WithJavaChestState(previous, pos, javaString(id, "type"))
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
	b = applyJavaActorState(b, id)
	_, oldActor := old.(NBTer)
	name := javaName(id)
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
		context := javaShapeContext{tx: tx, pos: pos}
		javaLiquidBlockOnPlace(&context, id)
	}
	return true
}
