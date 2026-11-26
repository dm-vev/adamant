package entity

import (
	"math"

	"github.com/dm-vev/adamant/server/block/cube"
)

// flyNodeEvaluator finds the nodes a flying mob may move to, ported from
// Java's FlyNodeEvaluator. It shares the walk evaluator of its navigation,
// whose fly flag gives blocks the path types of flight.
type flyNodeEvaluator struct{ *walkNodeEvaluator }

// start ports FlyNodeEvaluator.getStart: the block the mob flies in, or the
// surface of the water it floats in. A small mob that may not start there
// tries ten random blocks around it.
func (w flyNodeEvaluator) start() *pathNodeData {
	m := w.m
	x, z := int(math.Floor(m.data.Pos[0])), int(math.Floor(m.data.Pos[2]))
	var startY int
	if w.canFloat && m.InWater() {
		startY = int(math.Floor(m.data.Pos[1]))
		for pathWater(w.pw, cube.Pos{x, startY, z}) {
			startY++
		}
	} else {
		startY = int(math.Floor(m.data.Pos[1] + .5))
	}
	if !w.canFlyFrom(x, startY, z) {
		var buf [10]cube.Pos
		n := w.startCandidates(&buf)
		for i := range n {
			pos := buf[i]
			if w.canFlyFrom(pos[0], pos[1], pos[2]) {
				return w.startNode(pos[0], pos[1], pos[2])
			}
		}
	}
	return w.startNode(x, startY, z)
}

// canFlyFrom ports FlyNodeEvaluator.canStartAt.
func (w flyNodeEvaluator) canFlyFrom(x, y, z int) bool {
	return w.scratch.malus[w.cachedPathType(x, y, z)] >= 0
}

// startCandidates ports
// FlyNodeEvaluator.iteratePathfindingStartNodeCandidatePositions: the
// corners of a big mob, or ten random blocks in the box of a small mob grown
// to a block and a tenth.
func (w flyNodeEvaluator) startCandidates(out *[10]cube.Pos) int {
	m := w.m
	bb := m.BBox()
	loV, hiV := bb.Min(), bb.Max()
	if (bb.Width()+bb.Height()+bb.Length())/3 >= 1 {
		y := w.mobPos.Y()
		out[0] = cube.Pos{int(math.Floor(loV[0])), y, int(math.Floor(loV[2]))}
		out[1] = cube.Pos{int(math.Floor(loV[0])), y, int(math.Floor(hiV[2]))}
		out[2] = cube.Pos{int(math.Floor(hiV[0])), y, int(math.Floor(loV[2]))}
		out[3] = cube.Pos{int(math.Floor(hiV[0])), y, int(math.Floor(hiV[2]))}
		return 4
	}
	xPad, yPad, zPad := math.Max(0, 1.1-bb.Width()), math.Max(0, 1.1-bb.Height()), math.Max(0, 1.1-bb.Length())
	lo := cube.Pos{int(math.Floor(loV[0] - xPad)), int(math.Floor(loV[1] - yPad)), int(math.Floor(loV[2] - zPad))}
	hi := cube.Pos{int(math.Floor(hiV[0] + xPad)), int(math.Floor(hiV[1] + yPad)), int(math.Floor(hiV[2] + zPad))}
	r := m.Random()
	dx, dy, dz := int32(hi[0]-lo[0]+1), int32(hi[1]-lo[1]+1), int32(hi[2]-lo[2]+1)
	for i := range 10 {
		// BlockPos.randomBetweenClosed.
		x := lo[0] + int(r.NextInt(dx))
		y := lo[1] + int(r.NextInt(dy))
		z := lo[2] + int(r.NextInt(dz))
		out[i] = cube.Pos{x, y, z}
	}
	return 10
}

func flyPassable(n *pathNodeData) bool { return n != nil && n.costMalus >= 0 }

func flyAdd0(out []*pathNodeData, count *int, n *pathNodeData) *pathNodeData {
	if n != nil && !n.closed {
		out[*count] = n
		*count++
	}
	return n
}

func flyAdd2(out []*pathNodeData, count *int, n, a, b *pathNodeData) *pathNodeData {
	if n != nil && !n.closed && flyPassable(a) && flyPassable(b) {
		out[*count] = n
		*count++
	}
	return n
}

func flyAdd6(out []*pathNodeData, count *int, n, a, b, c, d, e, f *pathNodeData) {
	if n != nil && !n.closed && flyPassable(a) && flyPassable(b) && flyPassable(c) && flyPassable(d) && flyPassable(e) && flyPassable(f) {
		out[*count] = n
		*count++
	}
}

// neighbours ports FlyNodeEvaluator.getNeighbors: the six blocks around a
// node, the twelve on its edges and the eight on its corners, the last ones
// only where the blocks between are passable.
func (w flyNodeEvaluator) neighbours(out []*pathNodeData, pos *pathNodeData) int {
	count := 0
	x, y, z := pos.x, pos.y, pos.z
	south := flyAdd0(out, &count, w.flyAcceptedNode(x, y, z+1))
	west := flyAdd0(out, &count, w.flyAcceptedNode(x-1, y, z))
	east := flyAdd0(out, &count, w.flyAcceptedNode(x+1, y, z))
	north := flyAdd0(out, &count, w.flyAcceptedNode(x, y, z-1))
	up := flyAdd0(out, &count, w.flyAcceptedNode(x, y+1, z))
	down := flyAdd0(out, &count, w.flyAcceptedNode(x, y-1, z))
	southUp := flyAdd2(out, &count, w.flyAcceptedNode(x, y+1, z+1), south, up)
	westUp := flyAdd2(out, &count, w.flyAcceptedNode(x-1, y+1, z), west, up)
	eastUp := flyAdd2(out, &count, w.flyAcceptedNode(x+1, y+1, z), east, up)
	northUp := flyAdd2(out, &count, w.flyAcceptedNode(x, y+1, z-1), north, up)
	southDown := flyAdd2(out, &count, w.flyAcceptedNode(x, y-1, z+1), south, down)
	westDown := flyAdd2(out, &count, w.flyAcceptedNode(x-1, y-1, z), west, down)
	eastDown := flyAdd2(out, &count, w.flyAcceptedNode(x+1, y-1, z), east, down)
	northDown := flyAdd2(out, &count, w.flyAcceptedNode(x, y-1, z-1), north, down)
	northEast := flyAdd2(out, &count, w.flyAcceptedNode(x+1, y, z-1), north, east)
	southEast := flyAdd2(out, &count, w.flyAcceptedNode(x+1, y, z+1), south, east)
	northWest := flyAdd2(out, &count, w.flyAcceptedNode(x-1, y, z-1), north, west)
	southWest := flyAdd2(out, &count, w.flyAcceptedNode(x-1, y, z+1), south, west)
	flyAdd6(out, &count, w.flyAcceptedNode(x+1, y+1, z-1), northEast, north, east, up, northUp, eastUp)
	flyAdd6(out, &count, w.flyAcceptedNode(x+1, y+1, z+1), southEast, south, east, up, southUp, eastUp)
	flyAdd6(out, &count, w.flyAcceptedNode(x-1, y+1, z-1), northWest, north, west, up, northUp, westUp)
	flyAdd6(out, &count, w.flyAcceptedNode(x-1, y+1, z+1), southWest, south, west, up, southUp, westUp)
	flyAdd6(out, &count, w.flyAcceptedNode(x+1, y-1, z-1), northEast, north, east, down, northDown, eastDown)
	flyAdd6(out, &count, w.flyAcceptedNode(x+1, y-1, z+1), southEast, south, east, down, southDown, eastDown)
	flyAdd6(out, &count, w.flyAcceptedNode(x-1, y-1, z-1), northWest, north, west, down, northDown, westDown)
	flyAdd6(out, &count, w.flyAcceptedNode(x-1, y-1, z+1), southWest, south, west, down, southDown, westDown)
	return count
}

// flyAcceptedNode ports FlyNodeEvaluator.findAcceptedNode: a block the mob
// may fly through, where flying right above the ground costs a little more.
func (w flyNodeEvaluator) flyAcceptedNode(x, y, z int) *pathNodeData {
	t := w.cachedPathType(x, y, z)
	cost := w.scratch.malus[t]
	if cost < 0 {
		return nil
	}
	n := w.node(x, y, z)
	n.typ = t
	n.costMalus = max(n.costMalus, float32(cost))
	if t == PathWalkable {
		n.costMalus++
	}
	return n
}

func (r *pathStateReader) flyPathType(mobPos, pos cube.Pos) PathType {
	t := r.stateAt(pos[0], pos[1], pos[2])
	if t == PathOpen && pos[1] >= r.minY+1 {
		below := cube.Pos{pos[0], pos[1] - 1, pos[2]}
		switch belowType := r.stateAt(below[0], below[1], below[2]); belowType {
		case PathFire, PathLava:
			t = PathFire
		case PathDamaging:
			t = PathDamaging
		case PathCocoa:
			t = PathCocoa
		case PathFence:
			if below != mobPos {
				t = PathFence
			}
		case PathWalkable, PathOpen, PathWater:
		default:
			t = PathWalkable
		}
	}
	if t == PathWalkable || t == PathOpen {
		t = r.checkNeighbourBlocks(pos, t)
	}
	return t
}

// flyPathType ports FlyNodeEvaluator.getPathType: open air takes on the
// danger of the block below it, and air right above the ground is walkable.
func flyPathType(m *Mob, pos cube.Pos) PathType {
	var r pathStateReader
	r.initFromWorld(m.tx)
	return r.flyPathType(m.BlockPos(), pos)
}
