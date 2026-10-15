package world

import (
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// promoteGeneration only considers columns already demanded by simulation. A
// FULL neighbour may be inspected or saved without executing its markers.
func (t ticker) promoteGeneration(tx *Tx, loaders []*Loader) {
	w := tx.World()
	radius := int32(w.tickRange())
	if radius == 0 {
		return
	}
	areas := w.scratchLoaderAreas[:0]
	for _, loader := range loaders {
		areas = append(areas, loader.activeArea(radius))
	}
	columns := w.scratchPostProcessingColumns[:0]
	if w.conf.Synchronous {
		for pos, col := range w.chunks {
			if col.Ready() && w.generationPending(pos, col) {
				columns = append(columns, columnRef{pos: pos, col: col})
			}
		}
		slices.SortFunc(columns, func(a, b columnRef) int {
			if a.pos[0] < b.pos[0] || a.pos[0] == b.pos[0] && a.pos[1] < b.pos[1] {
				return -1
			}
			if a.pos != b.pos {
				return 1
			}
			return 0
		})
	} else {
		for _, ref := range w.activeColumns {
			if ref.col.Ready() && columnWithinAreas(ref.pos, areas) && w.generationPending(ref.pos, ref.col) {
				columns = append(columns, ref)
			}
		}
	}
	for _, ref := range columns {
		if !w.generationNeighbourhoodReady(ref.pos) {
			continue
		}
		for dx := range 3 {
			for dz := range 3 {
				w.finishGeneration(tx, w.chunks[ChunkPos{ref.pos[0] + int32(dx) - 1, ref.pos[1] + int32(dz) - 1}])
			}
		}
		w.postProcessGeneration(tx, ref.pos, ref.col)
		w.scheduledBlockTicks.activateChunk(ref.pos, w.scheduledBlockTicks.currentTick)
		w.scheduledFluidTicks.activateChunk(ref.pos, w.scheduledFluidTicks.currentTick)
	}
	clear(columns)
	w.scratchPostProcessingColumns = columns[:0]
	w.scratchLoaderAreas = areas[:0]
}

func (w *World) generationPending(pos ChunkPos, col *Column) bool {
	return col.postProcessing != nil || col.generation.PostProcessing != nil || len(col.generation.ScheduledTicks) != 0 ||
		w.scheduledBlockTicks.deferredChunks[pos] != 0 || w.scheduledFluidTicks.deferredChunks[pos] != 0
}

func (w *World) generationNeighbourhoodReady(pos ChunkPos) bool {
	for dx := range 3 {
		for dz := range 3 {
			col := w.chunks[ChunkPos{pos[0] + int32(dx) - 1, pos[1] + int32(dz) - 1}]
			if col == nil || !col.Ready() {
				return false
			}
		}
	}
	return true
}

// entityTickingReady reports whether the entities of the chunk at pos tick:
// Java ticks them only once the chunks within two chunks of it are generated.
// A synchronous world generates the chunks it reads on demand instead.
func (w *World) entityTickingReady(pos ChunkPos) bool {
	if w.conf.Synchronous {
		return true
	}
	for dx := range int32(5) {
		for dz := range int32(5) {
			col := w.chunks[ChunkPos{pos[0] + dx - 2, pos[1] + dz - 2}]
			if col == nil || !col.Ready() {
				return false
			}
		}
	}
	return true
}

func (w *World) postProcessGeneration(tx *Tx, pos ChunkPos, col *Column) {
	context := javaShapeContext{tx: tx}
	for section, positions := range col.postProcessing {
		for _, packed := range positions {
			context.pos = cube.Pos{int(pos[0])<<4 | int(packed&15), tx.Range().Min() + section*16 + int(packed>>4&15), int(pos[1])<<4 | int(packed>>8&15)}
			// The source captures this state before ticking its fluid. Do not
			// replace it with a second read after the fluid has mutated the cell.
			state := context.at(context.pos)
			fluid := javaFluidForState(state)
			if fluid.kind != javaFluidEmpty {
				javaTickFluid(&context, javaFluidBlock{state: state}, fluid)
			}
			name := javaName(state)
			if name == "water" || name == "lava" {
				javaTickLiquidBlock(&context, state)
			} else if updated := context.updateFromNeighbourShapes(state); updated != state {
				context.set(context.pos, updated, 276)
			}
		}
		col.postProcessing[section] = nil
	}
	col.postProcessing = nil
	col.modified = true
}
