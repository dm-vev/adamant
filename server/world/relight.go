package world

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/parallel"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// cellLightAt returns the light properties of the blocks of the column c at
// pos, as the light engine reads them.
func (w *World) cellLightAt(c *Column, pos cube.Pos) chunk.LightProperties {
	x, y, z := uint8(pos[0]), int16(pos[1]), uint8(pos[2])
	fg, bg := c.Block(x, y, z, 0), c.Block(x, y, z, 1)
	if lr, ok := w.conf.Blocks.(chunk.LightRegistry); ok {
		return lr.LightProperties(0, fg, bg)
	}
	br := w.conf.Blocks
	return chunk.LightProperties{
		Emission:  max(br.LightBlock(fg), br.LightBlock(bg)),
		Dampening: max(br.FilteringBlock(fg), br.FilteringBlock(bg)),
	}
}

// relightIfChanged queues the light around pos to be updated if the blocks
// there now affect light differently than before.
func (w *World) relightIfChanged(c *Column, pos cube.Pos, before chunk.LightProperties) {
	if w.cellLightAt(c, pos) == before {
		return
	}
	if w.pendingLight == nil {
		w.pendingLight = make(map[ChunkPos][]cube.Pos)
	}
	centre := chunkPosFromBlockPos(pos)
	w.pendingLight[centre] = append(w.pendingLight[centre], pos)
}

// flushLight brings the light up to date with the blocks changed since it
// last was. Changes are relit together per chunk, so that many blocks
// changed at once, as a floor being laid, are relit in one go.
func (w *World) flushLight() {
	if w.inRegionPhase() {
		// Light spreads across the columns of regions: the owner brings it
		// up to date after the phase, and regions read it as it was.
		return
	}
	w.flushSpreads()
	if len(w.pendingLight) == 0 {
		return
	}
	pending := w.pendingLight
	w.pendingLight = nil
	if len(pending) > 1 && w.relightTogether(pending) {
		return
	}
	for centre, changed := range pending {
		w.relight(centre, changed)
	}
}

// maxRelightArea is the most chunks across the changes of several chunks
// may span to be relit as one area.
const maxRelightArea = 8

// relightTogether relights the changes of several chunks in one area that
// spans them and the chunks around them, so that light removed by the
// change of one chunk is not spread back and removed again by that of its
// neighbour. It reports false, relighting nothing, if the changes span too
// many chunks or the area is not all lit.
func (w *World) relightTogether(pending map[ChunkPos][]cube.Pos) bool {
	minX, minZ, maxX, maxZ := int32(math.MaxInt32), int32(math.MaxInt32), int32(math.MinInt32), int32(math.MinInt32)
	for pos := range pending {
		minX, minZ = min(minX, pos[0]), min(minZ, pos[1])
		maxX, maxZ = max(maxX, pos[0]), max(maxZ, pos[1])
	}
	size := max(maxX-minX, maxZ-minZ) + 1
	if size > maxRelightArea {
		return false
	}
	// A square area with a margin of a chunk, which light changes reach.
	width := size + 2
	baseX, baseZ := minX-1, minZ-1
	area := make([]*chunk.Chunk, 0, width*width)
	for z := int32(0); z < width; z++ {
		for x := int32(0); x < width; x++ {
			col, ok := w.chunks[ChunkPos{baseX + x, baseZ + z}]
			if !ok || !col.Ready() || !col.lightReady.Load() {
				return false
			}
			area = append(area, col.Chunk)
		}
	}
	var changed []cube.Pos
	for _, positions := range pending {
		changed = append(changed, positions...)
	}
	chunk.LightArea(area, int(baseX), int(baseZ)).Relight(changed)
	return true
}

// relight updates the light around positions in the chunk at centre whose
// blocks changed in the light they emit or filter. Light crosses into
// neighbouring chunks if they are lit; otherwise it is spread to them once
// they are, by spreadLight.
func (w *World) relight(centre ChunkPos, changed []cube.Pos) {
	col, ok := w.chunks[centre]
	if !ok || !col.lightReady.Load() {
		// The light of the chunk is yet to be filled from its blocks.
		return
	}
	area := make([]*chunk.Chunk, 0, 9)
	for z := int32(-1); z <= 1 && area != nil; z++ {
		for x := int32(-1); x <= 1; x++ {
			neighbour, ok := w.chunks[ChunkPos{centre[0] + x, centre[1] + z}]
			if !ok || !neighbour.Ready() || !neighbour.lightReady.Load() {
				area = nil
				break
			}
			area = append(area, neighbour.Chunk)
		}
	}
	if area == nil {
		chunk.LightArea([]*chunk.Chunk{col.Chunk}, int(centre[0]), int(centre[1])).Relight(changed)
		return
	}
	chunk.LightArea(area, int(centre[0])-1, int(centre[1])-1).Relight(changed)
}

// relightChunks recomputes the light of the chunks passed after many of
// their blocks changed at once, and of the chunks around them that their
// light reaches into.
func (w *World) relightChunks(changed []ChunkPos) {
	refill := make(map[ChunkPos]struct{}, len(changed)*9)
	for _, pos := range changed {
		for x := int32(-1); x <= 1; x++ {
			for z := int32(-1); z <= 1; z++ {
				refill[ChunkPos{pos[0] + x, pos[1] + z}] = struct{}{}
			}
		}
	}
	type fill struct {
		pos ChunkPos
		c   *chunk.Chunk
	}
	fills := make([]fill, 0, len(refill))
	for pos := range refill {
		// Filling the chunk takes in every change pending in it.
		delete(w.pendingLight, pos)
		col, ok := w.chunks[pos]
		if !ok || !col.lightReady.Load() {
			continue
		}
		fills = append(fills, fill{pos: pos, c: col.Chunk})
	}
	// Each chunk is filled on its own, so they are filled on all cores.
	parallel.For(len(fills), 1, func(i int) {
		chunk.LightArea([]*chunk.Chunk{fills[i].c}, int(fills[i].pos[0]), int(fills[i].pos[1])).Fill()
	})
	spread := make(map[ChunkPos]struct{}, len(fills)*2)
	for _, f := range fills {
		for x := int32(-1); x <= 1; x++ {
			for z := int32(-1); z <= 1; z++ {
				spread[ChunkPos{f.pos[0] + x, f.pos[1] + z}] = struct{}{}
			}
		}
	}
	for pos := range spread {
		if _, ok := w.chunks[pos]; ok {
			w.spreadLight(pos)
		}
	}
	w.flushSpreads()
}

// LightBlockedInto ports LightEngine.getLightBlockInto for light passing
// from the block at pos into its neighbour on the face passed: 16 where the
// faces of the two blocks occlude it, or else the dampening of the
// neighbour.
func (tx *Tx) LightBlockedInto(pos cube.Pos, face cube.Face) int {
	w := tx.World()
	to := pos.Side(face)
	if to.OutOfBounds(w.Range()) || pos.OutOfBounds(w.Range()) {
		return 0
	}
	from, into := w.cellLightAt(tx.chunk(chunkPosFromBlockPos(pos)), pos), w.cellLightAt(tx.chunk(chunkPosFromBlockPos(to)), to)
	if chunk.FacesOcclude(from, into, face) {
		return 16
	}
	return int(into.Dampening)
}
