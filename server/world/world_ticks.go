package world

import (
	"fmt"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// scheduleBlockUpdate schedules a live update after a specific delay. Liquids
// are stored in the distinct fluid queue; other blocks use the block queue.
func (w *World) scheduleBlockUpdate(pos cube.Pos, b Block, delay time.Duration) {
	if pos.OutOfBounds(w.Range()) {
		return
	}
	queue := w.scheduledBlockTicks
	if _, ok := b.(Liquid); ok {
		queue = w.scheduledFluidTicks
	}
	if queue.schedule(w.conf.Blocks, pos, b, delay) {
		w.markScheduledTickColumnModified(pos)
	}
}

func (w *World) markScheduledTickColumnModified(pos cube.Pos) {
	if col, ok := w.chunks[chunkPosFromBlockPos(pos)]; ok {
		col.modified = true
	}
}

// resolveScheduledTickBlock maps a Java registry type back to a registered
// block implementation for dispatching a persisted or generated tick.
func (w *World) resolveScheduledTickBlock(tick chunk.ScheduledTick) (Block, bool) {
	if tick.Kind != chunk.BlockTick && tick.Kind != chunk.FluidTick {
		return nil, false
	}
	if tick.Kind == chunk.FluidTick && tick.Type == "minecraft:empty" {
		// EmptyFluid's legacy block is native air. It still owns a Source tick
		// record, even though dispatch against an empty fluid changes no blocks.
		return w.conf.Blocks.BlockByRuntimeID(w.conf.Blocks.AirRuntimeID())
	}
	key := scheduledTickTypeKey{kind: tick.Kind, typeName: tick.Type}
	if b, ok := w.scheduledTickBlocks[key]; ok {
		return b, true
	}
	if tick.Kind == chunk.BlockTick {
		if _, err := gamedata.BlockStateID(tick.Type, nil); err == nil {
			native, err := ResolveState(w.conf.Blocks, BlockState{Name: tick.Type})
			if err == nil {
				if b, ok := w.conf.Blocks.BlockByRuntimeID(native.Block); ok {
					if w.scheduledTickBlocks == nil {
						w.scheduledTickBlocks = make(map[scheduledTickTypeKey]Block)
					}
					w.scheduledTickBlocks[key] = b
					return b, true
				}
			}
		}
	}
	for rid := uint32(0); rid < uint32(w.conf.Blocks.BlockCount()); rid++ {
		b, ok := w.conf.Blocks.BlockByRuntimeID(rid)
		if !ok {
			continue
		}
		var typeName string
		if tick.Kind == chunk.FluidTick {
			liquid, ok := b.(Liquid)
			if !ok {
				continue
			}
			typeName = scheduledFluidTickType(liquid)
		} else {
			typeName = scheduledTickType(w.conf.Blocks, b)
		}
		if typeName == tick.Type {
			if w.scheduledTickBlocks == nil {
				w.scheduledTickBlocks = make(map[scheduledTickTypeKey]Block)
			}
			w.scheduledTickBlocks[key] = b
			return b, true
		}
	}
	return nil, false
}

// scheduleGeneratedTick installs a worldless generation result on the owner
// thread. Source ProtoChunkTicks converts every generated delay to zero.
func (w *World) scheduleGeneratedTick(tick chunk.ScheduledTick) {
	_, ok := w.resolveScheduledTickBlock(tick)
	if !ok {
		panic(fmt.Sprintf("generated tick: unknown %v type %q", tick.Kind, tick.Type))
	}
	queue := w.scheduledBlockTicks
	if tick.Kind == chunk.FluidTick {
		queue = w.scheduledFluidTicks
	}
	if queue.insertTypeTick(tick.Pos, tick.Type, 0, tick.Priority, true) {
		w.markScheduledTickColumnModified(tick.Pos)
	}
}

// neighbourUpdate represents a position that needs to be updated because of a
// neighbour that changed.
type neighbourUpdate struct {
	pos, neighbour cube.Pos
}

// updateNeighbour ticks the position passed as a result of the neighbour
// passed being updated.
func (w *World) updateNeighbour(pos, changedNeighbour cube.Pos) {
	w.neighbourUpdates.push(neighbourUpdate{pos: pos, neighbour: changedNeighbour})
}

func (w *World) addActiveColumn(pos ChunkPos, col *Column) {
	if w.activeColumnIndex == nil {
		w.activeColumnIndex = make(map[ChunkPos]int)
	}
	if idx, ok := w.activeColumnIndex[pos]; ok {
		w.activeColumns[idx].col = col
		return
	}
	w.activeColumns = append(w.activeColumns, columnRef{pos: pos, col: col})
	w.activeColumnIndex[pos] = len(w.activeColumns) - 1
}

func (w *World) removeActiveColumn(pos ChunkPos) {
	if len(w.activeColumns) == 0 {
		return
	}
	idx, ok := w.activeColumnIndex[pos]
	if !ok {
		return
	}
	last := len(w.activeColumns) - 1
	if idx != last {
		w.activeColumns[idx] = w.activeColumns[last]
		w.activeColumnIndex[w.activeColumns[idx].pos] = idx
	}
	w.activeColumns = w.activeColumns[:last]
	delete(w.activeColumnIndex, pos)
}
