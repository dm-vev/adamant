package chunk

import (
	"encoding/binary"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
)

// insertBlockLightNodes iterates over the chunk and looks for blocks that have a light level of at least 1.
// If one is found, a node is added for it to the node queue.
func (a *lightArea) insertBlockLightNodes(queue *levelQueue) {
	a.iterSubChunks(a.anyLightBlocks, func(pos cube.Pos) {
		if level := a.props(pos).Emission; level > 0 {
			queue.push(node(pos, level, BlockLight))
		}
	})
}

// anyLightBlocks checks if there are any blocks in the SubChunk passed that emit light.
func (a *lightArea) anyLightBlocks(sub *SubChunk) bool {
	for _, layer := range sub.storages {
		for _, id := range layer.palette.values {
			if a.cell(0, id, a.br.AirRuntimeID()).Emission != 0 || a.br.LightBlock(id) != 0 {
				return true
			}
		}
	}
	return false
}

// insertSkyLightNodes fills every column of the chunk with full skylight
// from its lowest sky source up, as ChunkSkyLightSources finds it, and
// inserts nodes for the sources whose light spreads into the column itself
// or into neighbouring columns with lower sources.
func (a *lightArea) insertSkyLightNodes(queue *levelQueue) {
	c := a.c[0]
	highestY := c.Range().Min()
	for index := range c.sub {
		if !c.sub[index].Empty() {
			highestY = int(c.SubY(int16(index))) + 15
		}
	}
	var sources [256]int
	for x := 0; x < 16; x++ {
		for z := 0; z < 16; z++ {
			sources[x<<4|z] = a.skySource(a.baseX+x, a.baseZ+z, highestY)
		}
	}
	for x := 0; x < 16; x++ {
		for z := 0; z < 16; z++ {
			source := sources[x<<4|z]
			highestNeighbour := source
			for _, n := range [4][2]int{{x - 1, z}, {x + 1, z}, {x, z - 1}, {x, z + 1}} {
				if n[0] >= 0 && n[0] < 16 && n[1] >= 0 && n[1] < 16 {
					highestNeighbour = max(highestNeighbour, sources[n[0]<<4|n[1]])
				}
			}
			for y := source; y <= highestY; y++ {
				pos := cube.Pos{a.baseX + x, y, a.baseZ + z}
				// The lowest source lights what lies below it, and sources
				// next to lower columns light those.
				if y == source || y < highestNeighbour {
					queue.push(node(pos, 15, SkyLight))
					continue
				}
				a.setLight(pos, SkyLight, 15)
			}
		}
	}
}

// skySource ports ChunkSkyLightSources: the lowest Y of the column at x, z
// that skylight reaches undimmed. Skylight falls from above the highest
// block until it meets a block that dampens it or a face that occludes it.
func (a *lightArea) skySource(x, z, highestY int) int {
	above := a.air
	for y := min(highestY, a.r.Max()); y >= a.r.Min(); y-- {
		below := a.props(cube.Pos{x, y, z})
		if skyEdgeOccluded(above, below) {
			return y + 1
		}
		above = below
	}
	return a.r.Min()
}

// skyEdgeOccluded ports ChunkSkyLightSources.isEdgeOccluded.
func skyEdgeOccluded(top, bottom LightProperties) bool {
	return bottom.Dampening != 0 || facesOcclude(top, bottom, cube.FaceDown)
}

// facesOcclude ports LightEngine.shapeOccludes for light going from a cell
// to its neighbour on the face passed: the face of the first and the
// opposite face of the second block it if together they cover the face.
func facesOcclude(from, to LightProperties, face cube.Face) bool {
	opposite := face.Opposite()
	if from.Occlusion&(1<<face) != 0 || to.Occlusion&(1<<opposite) != 0 {
		return true
	}
	if from.Partial&(1<<face) == 0 || to.Partial&(1<<opposite) == 0 {
		return false
	}
	a, _ := gamedata.BlockLightOcclusionFace(from.State, int(face))
	b, _ := gamedata.BlockLightOcclusionFace(to.State, int(opposite))
	for i := range a {
		if a[i]|b[i] != ^uint64(0) {
			return false
		}
	}
	return true
}

// insertLightSpreadingNodes inserts light nodes into the node queue passed which, when propagated, will
// spread into the neighbouring chunks. It walks the edges between the chunks of the lightArea sub chunk by sub
// chunk, skipping a 16x16 sheet of an edge if nothing can spread over it, and reads the light of both sides
// straight from the sub chunks.
func (a *lightArea) insertLightSpreadingNodes(queue *levelQueue, lt light) {
	needed := a.nodesNeeded(lt)
	_, sky := lt.(skyLight)
	lightOf := func(sub *SubChunk) []uint8 {
		if sky {
			return sub.skyLight
		}
		return sub.blockLight
	}
	minY, maxY := a.r[0]>>4, a.r[1]>>4
	for cu := 1; cu < a.w; cu++ {
		u := cu << 4
		for cv := 0; cv < a.w; cv++ {
			v := cv << 4
			for cy := minY; cy < maxY; cy++ {
				baseY := cy << 4
				// xa is at local x 0 of its sub chunk and xb, west of it, at local x 15 of its own; za and zb
				// are at local z 0 and 15 the same way.
				xa, za := cube.Pos{a.baseX + u, baseY, a.baseZ + v}, cube.Pos{a.baseX + v, baseY, a.baseZ + u}
				xb, zb := xa.Side(cube.FaceWest), za.Side(cube.FaceNorth)
				sxa, sxb, sza, szb := a.sub(xa), a.sub(xb), a.sub(za), a.sub(zb)
				addX, addZ := needed(sxa, sxb), needed(sza, szb)
				if !addX && !addZ {
					continue
				}
				// The light of the blocks at local x 0, and so of those at x
				// 15, of a sub chunk is 128 bytes in a row, a word of 16
				// blocks up for each z; that of the blocks at local z 0 or 15
				// is a word for each x, 128 bytes apart.
				if addX {
					a.spreadAcrossEdge(queue, lt, lightOf(sxa), lightOf(sxb), 0, 15*128, 8, xa, xb, cube.Pos{0, 0, 1})
				}
				if addZ {
					a.spreadAcrossEdge(queue, lt, lightOf(sza), lightOf(szb), 0, 15*8, 128, za, zb, cube.Pos{1, 0, 0})
				}
			}
		}
	}
}

// spreadAcrossEdge queues the nodes for light spreading across the edge
// between the sub chunks with the light la and lb, of which pa and pb are
// the bottom blocks of the first line up the edge on either side, the other
// lines following along step. The light of line v of either side is the word
// at offset plus v times stride of its light. Light only spreads where it
// differs, so lines with the same light on both sides are skipped whole.
func (a *lightArea) spreadAcrossEdge(queue *levelQueue, lt light, la, lb []uint8, offsetA, offsetB, stride int, pa, pb, step cube.Pos) {
	for v := range 16 {
		wa, wb := lightWord(la, offsetA+v*stride), lightWord(lb, offsetB+v*stride)
		if wa == wb {
			continue
		}
		along := cube.Pos{step[0] * v, 0, step[2] * v}
		for y := range 16 {
			if na, nb := uint8(wa>>(y<<2))&0xf, uint8(wb>>(y<<2))&0xf; na != nb {
				a.spreadAcross(queue, lt, na, nb, pa.Add(along).Add(cube.Pos{0, y}), pb.Add(along).Add(cube.Pos{0, y}))
			}
		}
	}
}

// lightWord returns the 16 nibbles of light at offset in light, the first in
// the lowest bits, or 0 if the light has none.
func lightWord(light []uint8, offset int) uint64 {
	if len(light) == 0 {
		return 0
	}
	return binary.LittleEndian.Uint64(light[offset : offset+8])
}

// spreadAcross queues a node for the light of la at pa entering pb, which
// has lb, or of lb entering pa, if either is bright enough to spread over.
func (a *lightArea) spreadAcross(queue *levelQueue, lt light, la, lb uint8, pa, pb cube.Pos) {
	if la == lb || la-1 == lb || lb-1 == la {
		// No chance for this to spread.
		return
	}
	if la > lb {
		if level, ok := a.entered(pa, pb, cube.FaceWest, la); ok && level > lb {
			queue.push(node(pb, level, lt))
		}
	} else if level, ok := a.entered(pb, pa, cube.FaceEast, lb); ok && level > la {
		queue.push(node(pa, level, lt))
	}
}

// nodesNeeded checks if any light nodes of a specific light type are needed between two neighbouring SubChunks when
// spreading light between them.
func (a *lightArea) nodesNeeded(lt light) func(sa, sb *SubChunk) bool {
	if lt == SkyLight {
		return func(sa, sb *SubChunk) bool {
			return &sa.skyLight[0] != &sb.skyLight[0]
		}
	}
	return func(sa, sb *SubChunk) bool {
		// Don't add nodes if both sub chunks are either both fully filled with light or have no light at all.
		return &sa.blockLight[0] != &sb.blockLight[0]
	}
}

// entered returns the light of the level passed after it entered to from
// from, as LightEngine.propagateIncrease: it loses max(1, dampening) of the
// block entered, and cannot pass faces that occlude it. face is the face of
// from that to lies on, or is derived from the positions if it is not one
// they lie on.
func (a *lightArea) entered(from, to cube.Pos, face cube.Face, level uint8) (uint8, bool) {
	if from.Side(face) != to {
		face = faceTowards(from, to)
	}
	toProps := a.props(to)
	opacity := max(1, toProps.Dampening)
	if level <= opacity {
		return 0, false
	}
	if facesOcclude(a.props(from), toProps, face) {
		return 0, false
	}
	return level - opacity, true
}

// faceTowards returns the face of from that the neighbouring to lies on.
func faceTowards(from, to cube.Pos) cube.Face {
	switch {
	case to[1] < from[1]:
		return cube.FaceDown
	case to[1] > from[1]:
		return cube.FaceUp
	case to[2] < from[2]:
		return cube.FaceNorth
	case to[2] > from[2]:
		return cube.FaceSouth
	case to[0] < from[0]:
		return cube.FaceWest
	}
	return cube.FaceEast
}

// propagate spreads the next light node in the node queue passed through the lightArea a. propagate adds the neighbours
// of the node to the queue for as long as it is able to spread.
func (a *lightArea) propagate(queue *levelQueue) {
	n, ok := queue.pop()
	if !ok {
		return
	}
	if a.light(n.pos, n.lt) >= n.level {
		return
	}
	a.setLight(n.pos, n.lt, n.level)
	a.changed |= 1 << a.chunkIndex((n.pos[0]-a.baseX)>>4, (n.pos[2]-a.baseZ)>>4)
	a.spreadFrom(queue, n.pos, n.lt, n.level)
}

// spreadFrom queues the neighbours of pos that the light of the level passed
// lights brighter than they are.
func (a *lightArea) spreadFrom(queue *levelQueue, pos cube.Pos, lt light, level uint8) {
	if level <= 1 {
		return
	}
	var from LightProperties
	fromRead := false
	for _, face := range cube.Faces() {
		to := pos.Side(face)
		if !a.inside(to) {
			continue
		}
		// Light loses at least a level entering a block, so a block lit as
		// bright already needs nothing read of it.
		current := a.light(to, lt)
		if current >= level-1 {
			continue
		}
		toProps := a.props(to)
		opacity := max(1, toProps.Dampening)
		if level <= opacity || current >= level-opacity {
			continue
		}
		if !fromRead {
			from, fromRead = a.props(pos), true
		}
		if facesOcclude(from, toProps, face) {
			continue
		}
		queue.push(node(to, level-opacity, lt))
	}
}

// lightNode is a node pushed to the queue which is used to propagate light.
type lightNode struct {
	pos   cube.Pos
	lt    light
	level uint8
}

// node creates a new lightNode using the position, level and light type passed.
func node(pos cube.Pos, level uint8, lt light) lightNode {
	return lightNode{pos: pos, level: level, lt: lt}
}

// FacesOcclude reports if light going from a cell with the properties from
// to its neighbour on the face passed is blocked by their faces, as
// LightEngine.shapeOccludes.
func FacesOcclude(from, to LightProperties, face cube.Face) bool {
	return facesOcclude(from, to, face)
}
