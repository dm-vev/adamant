package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
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
	if len(w.pendingLight) == 0 {
		return
	}
	pending := w.pendingLight
	w.pendingLight = nil
	for centre, changed := range pending {
		w.relight(centre, changed)
	}
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
	spread := make(map[ChunkPos]struct{}, len(refill)*2)
	for pos := range refill {
		// Filling the chunk takes in every change pending in it.
		delete(w.pendingLight, pos)
		col, ok := w.chunks[pos]
		if !ok || !col.lightReady.Load() {
			continue
		}
		chunk.LightArea([]*chunk.Chunk{col.Chunk}, int(pos[0]), int(pos[1])).Fill()
		for x := int32(-1); x <= 1; x++ {
			for z := int32(-1); z <= 1; z++ {
				spread[ChunkPos{pos[0] + x, pos[1] + z}] = struct{}{}
			}
		}
	}
	for pos := range spread {
		if _, ok := w.chunks[pos]; ok {
			w.spreadLight(pos)
		}
	}
}
