package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type lightProperties struct{ filter, emission uint8 }

func (w *World) cellLightProperties(c *Column, pos cube.Pos) lightProperties {
	x, y, z := uint8(pos[0]), int16(pos[1]), uint8(pos[2])
	a, b := c.Block(x, y, z, 0), c.Block(x, y, z, 1)
	return lightProperties{max(w.conf.Blocks.FilteringBlock(a), w.conf.Blocks.FilteringBlock(b)), max(w.conf.Blocks.LightBlock(a), w.conf.Blocks.LightBlock(b))}
}

func (w *World) queueLightChange(c *Column, pos cube.Pos, before lightProperties) {
	if before == w.cellLightProperties(c, pos) {
		return
	}
	if w.lightChanges == nil {
		w.lightChanges = make(map[cube.Pos]struct{})
	}
	w.lightChanges[pos] = struct{}{}
}

// flushLightChanges removes light that depended on the old cells, then floods
// from surviving neighbours and current emitters. All access stays on the world
// owner and in ready terrain. Mutations are batched until a light query or the
// transaction boundary, avoiding a complete reflood after each block in a fill.
func (w *World) flushLightChanges() {
	if len(w.lightChanges) == 0 {
		return
	}
	changed := w.lightChanges
	w.lightChanges = nil
	touched := make(map[*Column]struct{})
	for _, sky := range []bool{false, true} {
		w.updateLightChannel(changed, sky, touched)
	}
	for c := range touched {
		c.modified = true
		c.invalidateNetworkSubChunkPayloads()
	}
}

type removedLight struct {
	pos   cube.Pos
	level uint8
}

func (w *World) lightCell(pos cube.Pos) (*Column, *chunk.SubChunk, bool) {
	if pos.OutOfBounds(w.ra) {
		return nil, nil, false
	}
	c, ok := w.loadedChunk(chunkPosFromBlockPos(pos))
	if !ok {
		return nil, nil, false
	}
	return c, c.SubChunk(int16(pos[1])), true
}

func storedLight(s *chunk.SubChunk, p cube.Pos, sky bool) uint8 {
	x, y, z := uint8(p[0]&15), uint8(p[1]&15), uint8(p[2]&15)
	if sky {
		return s.SkyLight(x, y, z)
	}
	return s.BlockLight(x, y, z)
}

func writeStoredLight(s *chunk.SubChunk, p cube.Pos, sky bool, level uint8) {
	x, y, z := uint8(p[0]&15), uint8(p[1]&15), uint8(p[2]&15)
	if sky {
		s.SetSkyLight(x, y, z, level)
	} else {
		s.SetBlockLight(x, y, z, level)
	}
}

func (w *World) updateLightChannel(changed map[cube.Pos]struct{}, sky bool, touched map[*Column]struct{}) {
	removed := make([]removedLight, 0, len(changed))
	seeds := make(map[cube.Pos]struct{}, len(changed)*2)
	clearLight := func(c *Column, s *chunk.SubChunk, p cube.Pos, level uint8) {
		writeStoredLight(s, p, sky, 0)
		touched[c] = struct{}{}
		removed = append(removed, removedLight{p, level})
	}
	for p := range changed {
		if c, s, ok := w.lightCell(p); ok {
			if old := storedLight(s, p, sky); old > 0 {
				clearLight(c, s, p, old)
			}
			seeds[p] = struct{}{}
			for _, face := range cube.Faces() {
				seeds[p.Side(face)] = struct{}{}
			}
		}
	}
	for i := 0; i < len(removed); i++ {
		n := removed[i]
		for _, face := range cube.Faces() {
			p := n.pos.Side(face)
			c, s, ok := w.lightCell(p)
			if !ok {
				continue
			}
			level := storedLight(s, p, sky)
			if level == 0 {
				continue
			}
			if level < n.level || (sky && face == cube.FaceDown && n.level == 15 && level == 15) {
				clearLight(c, s, p, level)
				// An independent emitter may have been cleared along with the
				// dependent field. Seed its own light in the second pass.
			}
			seeds[p] = struct{}{}
		}
	}
	queue := make([]cube.Pos, 0, len(seeds))
	queued := make(map[cube.Pos]struct{}, len(seeds))
	push := func(p cube.Pos) {
		if _, ok := queued[p]; !ok {
			queue = append(queue, p)
			queued[p] = struct{}{}
		}
	}
	for p := range seeds {
		c, s, ok := w.lightCell(p)
		if !ok {
			continue
		}
		props := w.cellLightProperties(c, p)
		emit := props.emission
		if sky {
			emit = 0
			if p[1] == w.ra.Max() && props.filter == 0 {
				emit = 15
			}
		}
		if emit > storedLight(s, p, sky) {
			writeStoredLight(s, p, sky, emit)
			touched[c] = struct{}{}
		}
		if storedLight(s, p, sky) > 0 {
			push(p)
		}
	}
	for i := 0; i < len(queue); i++ {
		p := queue[i]
		delete(queued, p)
		_, s, _ := w.lightCell(p)
		level := storedLight(s, p, sky)
		for _, face := range cube.Faces() {
			next := p.Side(face)
			c, other, ok := w.lightCell(next)
			if !ok {
				continue
			}
			props := w.cellLightProperties(c, next)
			attenuation := max(props.filter, 1)
			var candidate uint8
			if sky && face == cube.FaceDown && level == 15 && props.filter == 0 {
				candidate = 15
			} else if level > attenuation {
				candidate = level - attenuation
			}
			if candidate > storedLight(other, next, sky) {
				writeStoredLight(other, next, sky, candidate)
				touched[c] = struct{}{}
				push(next)
			}
		}
	}
}
