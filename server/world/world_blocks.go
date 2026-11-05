package world

import (
	"context"
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// blockLoaded reads a block from a position only if its chunk is already loaded.
func (w *World) blockLoaded(pos cube.Pos) (Block, bool) {
	if pos.OutOfBounds(w.ra) {
		return w.conf.Blocks.Air(), false
	}
	c, ok := w.chunks[chunkPosFromBlockPos(pos)]
	if !ok || !c.Ready() {
		return w.conf.Blocks.Air(), false
	}
	return w.loadedBlockIn(c, pos), true
}

// loadedBlockIn reads the block at pos from the ready column c holding it
// without storing anything in c.
func (w *World) loadedBlockIn(c *Column, pos cube.Pos) Block {
	rid := c.Block(uint8(pos[0]), int16(pos[1]), uint8(pos[2]), 0)
	if w.conf.Blocks.NBTBlock(rid) {
		if b, ok := c.BlockEntities[pos]; ok {
			return b
		}
	}
	return w.conf.Blocks.BlockByRuntimeIDOrAir(rid)
}

// reconcileMissingBlockStates applies only opted-in missing-state reconstruction. It runs on the world owner after a
// chunk becomes ready, and revisits pending states in the chunk and its surrounding chunks.
func (w *World) reconcileMissingBlockStates(pos ChunkPos) {
	type candidate struct {
		column     *Column
		pos        cube.Pos
		properties []string
		order      int
	}
	candidates := make([]candidate, 0)
	for _, offset := range [...]ChunkPos{{}, {-1, 0}, {1, 0}, {0, -1}, {0, 1}, {-1, -1}, {-1, 1}, {1, -1}, {1, 1}} {
		candidatePos := ChunkPos{pos[0] + offset[0], pos[1] + offset[1]}
		col, ok := w.chunks[candidatePos]
		if !ok || !col.Ready() || len(col.missingBlockStates) == 0 {
			continue
		}
		for blockPos, state := range col.missingBlockStates {
			current, loaded := w.blockLoaded(blockPos)
			if !loaded {
				continue
			}
			reconstructor, ok := current.(MissingStateReconstructor)
			if !ok {
				if col.CanonicalizeMissingBlockState(blockPos, 0) {
					col.modified = true
					col.blockVersion = blockVersionClock.Add(1)
					delete(col.missingBlockStates, blockPos)
				}
				continue
			}
			candidates = append(candidates, candidate{
				column: col, pos: blockPos, properties: state.properties,
				order: reconstructor.ReconstructionOrder(),
			})
		}
	}
	if len(candidates) == 0 {
		return
	}
	slices.SortFunc(candidates, func(a, b candidate) int {
		if a.order < b.order {
			return -1
		}
		if a.order > b.order {
			return 1
		}
		for _, axis := range [...]int{1, 0, 2} {
			if a.pos[axis] < b.pos[axis] {
				return -1
			}
			if a.pos[axis] > b.pos[axis] {
				return 1
			}
		}
		return 0
	})
	for _, candidate := range candidates {
		current, loaded := w.blockLoaded(candidate.pos)
		if !loaded {
			continue
		}
		reconstructor, ok := current.(MissingStateReconstructor)
		if !ok {
			continue
		}
		source := &loadedWorldSource{w: w, complete: true, order: candidate.order}
		updated, retained := w.retainedJavaBlock(candidate.column, candidate.pos, current)
		if !retained {
			updated = reconstructor.ReconstructMissingState(candidate.pos, candidate.properties, source)
		}
		if updated == nil || !source.complete {
			continue
		}
		rid := w.conf.Blocks.BlockRuntimeID(updated)
		oldRID := candidate.column.Block(uint8(candidate.pos[0]), int16(candidate.pos[1]), uint8(candidate.pos[2]), 0)
		if oldRID != rid {
			oldLight := w.cellLightAt(candidate.column, candidate.pos)
			candidate.column.SetBlock(uint8(candidate.pos[0]), int16(candidate.pos[1]), uint8(candidate.pos[2]), 0, rid)
			w.relightIfChanged(candidate.column, candidate.pos, oldLight)
			candidate.column.invalidateRandomTickSubChunks()
			candidate.column.invalidateSubChunkHeightMaps()
			candidate.column.invalidateNetworkSubChunkPayloads()
			candidate.column.modified = true
			w.viewBlockUpdate(candidate.column, candidate.pos, updated, 0)
		}
		if candidate.column.CanonicalizeMissingBlockState(candidate.pos, 0) {
			candidate.column.modified = true
			candidate.column.blockVersion = blockVersionClock.Add(1)
			delete(candidate.column.missingBlockStates, candidate.pos)
		}
	}
}

// blockInChunk reads a block from a chunk at the position passed. The block
// is assumed to be within the chunk passed.
func (w *World) blockInChunk(c *Column, pos cube.Pos) Block {
	if pos.OutOfBounds(w.ra) {
		// Fast way out.
		return w.conf.Blocks.Air()
	}
	rid := c.Block(uint8(pos[0]), int16(pos[1]), uint8(pos[2]), 0)
	if w.conf.Blocks.NBTBlock(rid) {
		// The block was also a block entity, so we look it up in the block entity map.
		if b, ok := c.BlockEntities[pos]; ok {
			return b
		}
		// Despite being a block with NBT, the block didn't actually have any
		// stored NBT yet. We add it here and update the block.
		nbtB := DecodeNBT(w.conf.Blocks.BlockByRuntimeIDOrAir(rid).(NBTer), map[string]any{}, w.conf.Blocks).(Block)
		nbtB = restoreJavaBlockActor(c.Chunk, pos, nbtB)
		c.updateBlockEntity(pos, nbtB)
		c.invalidateBlockEntityPayloads()
		w.viewBlockUpdate(c, pos, nbtB, 0)
		return nbtB
	}
	return w.conf.Blocks.BlockByRuntimeIDOrAir(rid)
}

// HighestLightBlocker gets the Y value of the highest fully light blocking
// block at the x and z values passed in the World. It must not be called from
// within a transaction; use Tx.HighestLightBlocker instead.
func (w *World) HighestLightBlocker(x, z int) int {
	y, _ := Call(context.Background(), w, func(tx *Tx) (int, error) {
		return tx.highestLightBlocker(x, z), nil
	})
	return y
}

func (w *World) replacePositionTrackingBlock(tx *Tx, pos cube.Pos, old, replacement Block) Block {
	oldTracked, oldOK := old.(PositionTrackingBlock)
	newTracked, newOK := replacement.(PositionTrackingBlock)
	if oldOK && oldTracked.TrackingHandle() != 0 && (!newOK || newTracked.TrackingHandle() != oldTracked.TrackingHandle()) {
		// Untracking tells every viewer of the world.
		tx.deferToOwner(func(*Tx) { w.UntrackPosition(pos) })
	}
	if newOK && newTracked.TrackingHandle() != 0 {
		return newTracked.WithTrackingHandle(w.TrackPosition(pos, newTracked.TrackingHandle()))
	}
	return replacement
}

// liquidInColumn returns the liquid at pos in any layer of the column c
// holding it.
func (w *World) liquidInColumn(c *Column, pos cube.Pos) (Liquid, bool) {
	x, y, z := uint8(pos[0]), int16(pos[1]), uint8(pos[2])

	id := c.Block(x, y, z, 0)
	if !w.conf.Blocks.LiquidBlock(id) {
		// Most blocks hold no liquid: only look the layers up as blocks if
		// the registry says one is a liquid.
		id = c.Block(x, y, z, 1)
		if !w.conf.Blocks.LiquidBlock(id) {
			return nil, false
		}
	}
	b, ok := w.conf.Blocks.BlockByRuntimeID(id)
	if !ok {
		w.log.Error("Liquid: no block with runtime ID", "ID", id)
		return nil, false
	}
	if liq, ok := b.(Liquid); ok {
		return liq, true
	}
	id = c.Block(x, y, z, 1)

	b, ok = w.conf.Blocks.BlockByRuntimeID(id)
	if !ok {
		w.log.Error("Liquid: no block with runtime ID", "ID", id)
		return nil, false
	}
	liq, ok := b.(Liquid)
	return liq, ok
}

// subChunksMayContainLiquid reports whether a palette of a layer of the sub
// chunks of c from minY to maxY, both within the world's range, holds a
// liquid.
func (w *World) subChunksMayContainLiquid(c *Column, minY, maxY int) bool {
	subs := c.Sub()
	for i := c.SubIndex(int16(minY)); i <= c.SubIndex(int16(maxY)); i++ {
		for _, layer := range subs[i].Layers() {
			palette := layer.Palette()
			for j := range palette.Len() {
				if w.conf.Blocks.LiquidBlock(palette.Value(uint16(j))) {
					return true
				}
			}
		}
	}
	return false
}

// removeLiquids removes any liquid blocks that may be present at a specific
// block position in the chunk passed. The bool returned specifies if no blocks
// were left on the foreground layer.
func (w *World) removeLiquids(c *Column, pos cube.Pos) bool {
	x, y, z := uint8(pos[0]), int16(pos[1]), uint8(pos[2])
	air := w.conf.Blocks.Air()

	noneLeft := false
	if noLeft, changed := w.removeLiquidOnLayer(c, x, y, z, 0); noLeft {
		if changed {
			w.viewBlockUpdate(c, pos, air, 0)
		}
		noneLeft = true
	}
	if _, changed := w.removeLiquidOnLayer(c, x, y, z, 1); changed {
		w.viewBlockUpdate(c, pos, air, 1)
	}
	return noneLeft
}

// removeLiquidOnLayer removes a liquid block from a specific layer in the
// chunk passed, returning true if successful.
func (w *World) removeLiquidOnLayer(c *Column, x uint8, y int16, z, layer uint8) (bool, bool) {
	id := c.Block(x, y, z, layer)
	airRID := w.conf.Blocks.AirRuntimeID()

	b, ok := w.conf.Blocks.BlockByRuntimeID(id)
	if !ok {
		w.log.Error("removeLiquidOnLayer: no block with runtime ID", "ID", id)
		return false, false
	}
	if _, ok := b.(Liquid); ok {
		c.SetBlock(x, y, z, layer, airRID)
		c.invalidateRandomTickSubChunks()
		c.invalidateSubChunkHeightMaps()
		c.invalidateNetworkSubChunkPayloads()
		return true, true
	}
	return id == airRID, false
}

// pendingMissingBlockState retains a partial state until its horizontal neighbours are available.
type pendingMissingBlockState struct {
	properties []string
}
