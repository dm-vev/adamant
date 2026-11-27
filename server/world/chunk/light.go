package chunk

import (
	"encoding/binary"

	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/internal/gamedata"
)

// insertBlockLightNodes iterates over the chunk and looks for blocks that have a light level of at least 1.
// If one is found, a node is added for it to the node queue.
func (a *lightArea) insertBlockLightNodes(queue *levelQueue) {
	c := a.c[0]
	for index, sub := range c.sub {
		if !a.anyLightBlocks(sub) {
			continue
		}
		baseY := int(c.SubY(int16(index)))
		s0 := sub.storages[0]
		if len(sub.storages) == 1 {
			for x := range 16 {
				for z := range 16 {
					for y := range 16 {
						if level := a.cell(0, s0.At(byte(x), byte(y), byte(z)), a.airRID).Emission; level > 0 {
							queue.push(lightNode{x: int32(a.baseX + x), y: int32(baseY + y), z: int32(a.baseZ + z), level: level})
						}
					}
				}
			}
			continue
		}
		s1 := sub.storages[1]
		for x := range 16 {
			for z := range 16 {
				for y := range 16 {
					if level := a.cell(0, s0.At(byte(x), byte(y), byte(z)), s1.At(byte(x), byte(y), byte(z))).Emission; level > 0 {
						queue.push(lightNode{x: int32(a.baseX + x), y: int32(baseY + y), z: int32(a.baseZ + z), level: level})
					}
				}
			}
		}
	}
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
	for x := range 16 {
		for z := range 16 {
			sources[x<<4|z] = a.skySourceInChunk(c, byte(x), byte(z), highestY)
		}
	}
	topSub := int(c.SubIndex(int16(min(highestY, a.r.Max()))))
	for x := range 16 {
		for z := range 16 {
			source := sources[x<<4|z]
			highestNeighbour := source
			for _, n := range [4][2]int{{x - 1, z}, {x + 1, z}, {x, z - 1}, {x, z + 1}} {
				if uint(n[0]) < 16 && uint(n[1]) < 16 {
					highestNeighbour = max(highestNeighbour, sources[n[0]<<4|n[1]])
				}
			}
			endQueue := min(highestNeighbour, highestY+1)
			if source <= highestY {
				queue.push(lightNode{x: int32(a.baseX + x), y: int32(source), z: int32(a.baseZ + z), level: 15, sky: true})
				for y := source + 1; y < endQueue; y++ {
					queue.push(lightNode{x: int32(a.baseX + x), y: int32(y), z: int32(a.baseZ + z), level: 15, sky: true})
				}
			}
			fillStart := max(source+1, highestNeighbour)
			if fillStart <= highestY {
				startSub := int(c.SubIndex(int16(fillStart)))
				for idx := startSub; idx <= topSub; idx++ {
					sub := c.sub[idx]
					subBase := int(c.SubY(int16(idx)))
					y0 := max(fillStart-subBase, 0)
					y1 := min(highestY-subBase, 15)
					setSkyLightColumnRange(sub, byte(x), byte(z), y0, y1)
				}
			}
		}
	}
}

func setSkyLightColumnRange(sub *SubChunk, x, z byte, y0, y1 int) {
	if y0 > y1 {
		return
	}
	if len(sub.skyLight) == 0 || &sub.skyLight[0] == noLightPtr {
		sub.skyLight = make([]uint8, 2048)
	} else if &sub.skyLight[0] == fullLightPtr {
		return
	}
	baseByte := (int(x) << 7) | (int(z) << 3)
	for y := y0; y <= y1; y++ {
		if y&1 == 0 && y+1 <= y1 {
			sub.skyLight[baseByte|(y>>1)] = 0xff
			y++
			continue
		}
		sub.skyLight[baseByte|(y>>1)] |= 0xf << ((uint(y) & 1) << 2)
	}
}

// skySource ports ChunkSkyLightSources: the lowest Y of the column at x, z
// that skylight reaches undimmed. Skylight falls from above the highest
// block until it meets a block that dampens it or a face that occludes it.
func (a *lightArea) skySource(x, z, highestY int) int {
	return a.skySourceInChunk(a.chunk(cube.Pos{x, 0, z}), byte(x&15), byte(z&15), highestY)
}

func (a *lightArea) skySourceInChunk(c *Chunk, lx, lz byte, highestY int) int {
	topY := min(highestY, a.r.Max())
	minY := a.r.Min()
	if topY < minY {
		return minY
	}
	above := a.air
	topSub := int(c.SubIndex(int16(topY)))
	for idx := topSub; idx >= 0; idx-- {
		sub := c.sub[idx]
		subBase := int(c.SubY(int16(idx)))
		yStart := min(topY, subBase+15)
		yEnd := max(minY, subBase)
		if yStart < yEnd {
			break
		}
		if len(sub.storages) == 0 || (len(sub.storages) == 1 && sub.storages[0].bitsPerIndex == 0) {
			rid := a.airRID
			if len(sub.storages) == 1 {
				rid = sub.storages[0].palette.values[0]
			}
			below := a.air
			if rid != a.airRID {
				below = a.cell(0, rid, a.airRID)
			}
			if skyEdgeOccluded(above, below) {
				return yStart + 1
			}
			if yStart > yEnd && below != a.air && skyEdgeOccluded(below, below) {
				return yStart
			}
			above = below
			continue
		}
		s0 := sub.storages[0]
		if len(sub.storages) == 1 {
			for y := yStart; y >= yEnd; y-- {
				below := a.cell(0, s0.At(lx, byte(y&15), lz), a.airRID)
				if skyEdgeOccluded(above, below) {
					return y + 1
				}
				above = below
			}
		} else {
			s1 := sub.storages[1]
			for y := yStart; y >= yEnd; y-- {
				below := a.cell(0, s0.At(lx, byte(y&15), lz), s1.At(lx, byte(y&15), lz))
				if skyEdgeOccluded(above, below) {
					return y + 1
				}
				above = below
			}
		}
	}
	return minY
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

func lightBasePtr(l []uint8) *uint8 {
	if len(l) == 0 {
		return noLightPtr
	}
	return &l[0]
}

// nodesNeeded checks if any light nodes of a specific light type are needed between two neighbouring SubChunks when
// spreading light between them.
func (a *lightArea) nodesNeeded(lt light) func(sa, sb *SubChunk) bool {
	if lt == SkyLight {
		return func(sa, sb *SubChunk) bool {
			return lightBasePtr(sa.skyLight) != lightBasePtr(sb.skyLight)
		}
	}
	return func(sa, sb *SubChunk) bool {
		// Don't add nodes if both sub chunks are either both fully filled with light or have no light at all.
		return lightBasePtr(sa.blockLight) != lightBasePtr(sb.blockLight)
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
	cIdx := a.chunkIndex((int(n.x)-a.baseX)>>4, (int(n.z)-a.baseZ)>>4)
	c := a.c[cIdx]
	sub := c.sub[c.SubIndex(int16(n.y))]
	lx, ly, lz := byte(n.x&0xf), byte(n.y&0xf), byte(n.z&0xf)
	if n.sky {
		if sub.SkyLight(lx, ly, lz) >= n.level {
			return
		}
		sub.SetSkyLight(lx, ly, lz, n.level)
	} else {
		if sub.BlockLight(lx, ly, lz) >= n.level {
			return
		}
		sub.SetBlockLight(lx, ly, lz, n.level)
	}
	a.changed |= 1 << cIdx
	a.spreadFromSub(queue, cube.Pos{int(n.x), int(n.y), int(n.z)}, sub, n.sky, n.level)
}

// spreadFrom queues the neighbours of pos that the light of the level passed
// lights brighter than they are.
func (a *lightArea) spreadFrom(queue *levelQueue, pos cube.Pos, lt light, level uint8) {
	if level <= 1 {
		return
	}
	a.spreadFromSub(queue, pos, a.sub(pos), lt == SkyLight, level)
}

var lightFaceOffsets = [6]cube.Pos{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

func (a *lightArea) spreadFromSub(queue *levelQueue, pos cube.Pos, centerSub *SubChunk, sky bool, level uint8) {
	if level <= 1 {
		return
	}
	lx0, ly0, lz0 := pos[0]&0xf, pos[1]&0xf, pos[2]&0xf
	var from LightProperties
	fromRead := false
	for face := cube.Face(0); face < 6; face++ {
		d := lightFaceOffsets[face]
		nx, ny, nz := pos[0]+d[0], pos[1]+d[1], pos[2]+d[2]
		if ny < a.r.Min() || ny > a.r.Max() || nx < a.baseX || nz < a.baseZ || nx >= a.baseX+a.w*16 || nz >= a.baseZ+a.w*16 {
			continue
		}
		nlx, nly, nlz := lx0+d[0], ly0+d[1], lz0+d[2]
		toSub := centerSub
		if uint(nlx|nly|nlz) >= 16 {
			toSub = a.sub(cube.Pos{nx, ny, nz})
		}
		var current uint8
		if sky {
			current = toSub.SkyLight(byte(nx&0xf), byte(ny&0xf), byte(nz&0xf))
		} else {
			current = toSub.BlockLight(byte(nx&0xf), byte(ny&0xf), byte(nz&0xf))
		}
		if current >= level-1 {
			continue
		}
		toProps := a.propsFromSub(toSub, byte(nx&0xf), byte(ny&0xf), byte(nz&0xf))
		opacity := max(1, toProps.Dampening)
		if level <= opacity || current >= level-opacity {
			continue
		}
		if !fromRead {
			from, fromRead = a.propsFromSub(centerSub, byte(lx0), byte(ly0), byte(lz0)), true
		}
		if facesOcclude(from, toProps, face) {
			continue
		}
		queue.push(lightNode{x: int32(nx), y: int32(ny), z: int32(nz), level: level - opacity, sky: sky})
	}
}

// lightNode is a node pushed to the queue which is used to propagate light.
type lightNode struct {
	x, y, z int32
	level   uint8
	sky     bool
}

func (n lightNode) pos() cube.Pos {
	return cube.Pos{int(n.x), int(n.y), int(n.z)}
}

// node creates a new lightNode using the position, level and light type passed.
func node(pos cube.Pos, level uint8, lt light) lightNode {
	_, sky := lt.(skyLight)
	return lightNode{x: int32(pos[0]), y: int32(pos[1]), z: int32(pos[2]), level: level, sky: sky}
}

// FacesOcclude reports if light going from a cell with the properties from
// to its neighbour on the face passed is blocked by their faces, as
// LightEngine.shapeOccludes.
func FacesOcclude(from, to LightProperties, face cube.Face) bool {
	return facesOcclude(from, to, face)
}
