package chunk

import (
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
// spread into the neighbouring chunks.
func (a *lightArea) insertLightSpreadingNodes(queue *levelQueue, lt light) {
	a.iterEdges(a.nodesNeeded(lt), func(pa, pb cube.Pos) {
		la, lb := a.light(pa, lt), a.light(pb, lt)
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
	})
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
