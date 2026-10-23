package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
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
		for pathWater(m.tx, cube.Pos{x, startY, z}) {
			startY++
		}
	} else {
		startY = int(math.Floor(m.data.Pos[1] + .5))
	}
	if !w.canFlyFrom(x, startY, z) {
		for _, pos := range w.startCandidates() {
			if w.canFlyFrom(pos[0], pos[1], pos[2]) {
				return w.startNode(pos[0], pos[1], pos[2])
			}
		}
	}
	return w.startNode(x, startY, z)
}

// canFlyFrom ports FlyNodeEvaluator.canStartAt.
func (w flyNodeEvaluator) canFlyFrom(x, y, z int) bool {
	return w.m.PathfindingMalus(w.cachedPathType(x, y, z)) >= 0
}

// startCandidates ports
// FlyNodeEvaluator.iteratePathfindingStartNodeCandidatePositions: the
// corners of a big mob, or ten random blocks in the box of a small mob grown
// to a block and a tenth.
func (w flyNodeEvaluator) startCandidates() []cube.Pos {
	m := w.m
	bb := m.BBox()
	min, max := bb.Min(), bb.Max()
	if (bb.Width()+bb.Height()+bb.Length())/3 >= 1 {
		y := m.BlockPos().Y()
		corner := func(x, z float64) cube.Pos { return cube.Pos{int(math.Floor(x)), y, int(math.Floor(z))} }
		return []cube.Pos{corner(min[0], min[2]), corner(min[0], max[2]), corner(max[0], min[2]), corner(max[0], max[2])}
	}
	xPad, yPad, zPad := math.Max(0, 1.1-bb.Width()), math.Max(0, 1.1-bb.Height()), math.Max(0, 1.1-bb.Length())
	lo := cube.Pos{int(math.Floor(min[0] - xPad)), int(math.Floor(min[1] - yPad)), int(math.Floor(min[2] - zPad))}
	hi := cube.Pos{int(math.Floor(max[0] + xPad)), int(math.Floor(max[1] + yPad)), int(math.Floor(max[2] + zPad))}
	r := m.Random()
	out := make([]cube.Pos, 10)
	for i := range out {
		// BlockPos.randomBetweenClosed.
		x := lo[0] + int(r.NextInt(int32(hi[0]-lo[0]+1)))
		y := lo[1] + int(r.NextInt(int32(hi[1]-lo[1]+1)))
		z := lo[2] + int(r.NextInt(int32(hi[2]-lo[2]+1)))
		out[i] = cube.Pos{x, y, z}
	}
	return out
}

// neighbours ports FlyNodeEvaluator.getNeighbors: the six blocks around a
// node, the twelve on its edges and the eight on its corners, the last ones
// only where the blocks between are passable.
func (w flyNodeEvaluator) neighbours(out []*pathNodeData, pos *pathNodeData) int {
	count := 0
	add := func(n *pathNodeData, between ...*pathNodeData) *pathNodeData {
		if n == nil || n.closed {
			return n
		}
		for _, b := range between {
			if b == nil || b.costMalus < 0 {
				return n
			}
		}
		out[count] = n
		count++
		return n
	}
	at := func(dx, dy, dz int) *pathNodeData { return w.flyAcceptedNode(pos.x+dx, pos.y+dy, pos.z+dz) }
	south := add(at(0, 0, 1))
	west := add(at(-1, 0, 0))
	east := add(at(1, 0, 0))
	north := add(at(0, 0, -1))
	up := add(at(0, 1, 0))
	down := add(at(0, -1, 0))
	southUp := add(at(0, 1, 1), south, up)
	westUp := add(at(-1, 1, 0), west, up)
	eastUp := add(at(1, 1, 0), east, up)
	northUp := add(at(0, 1, -1), north, up)
	southDown := add(at(0, -1, 1), south, down)
	westDown := add(at(-1, -1, 0), west, down)
	eastDown := add(at(1, -1, 0), east, down)
	northDown := add(at(0, -1, -1), north, down)
	northEast := add(at(1, 0, -1), north, east)
	southEast := add(at(1, 0, 1), south, east)
	northWest := add(at(-1, 0, -1), north, west)
	southWest := add(at(-1, 0, 1), south, west)
	add(at(1, 1, -1), northEast, north, east, up, northUp, eastUp)
	add(at(1, 1, 1), southEast, south, east, up, southUp, eastUp)
	add(at(-1, 1, -1), northWest, north, west, up, northUp, westUp)
	add(at(-1, 1, 1), southWest, south, west, up, southUp, westUp)
	add(at(1, -1, -1), northEast, north, east, down, northDown, eastDown)
	add(at(1, -1, 1), southEast, south, east, down, southDown, eastDown)
	add(at(-1, -1, -1), northWest, north, west, down, northDown, westDown)
	add(at(-1, -1, 1), southWest, south, west, down, southDown, westDown)
	return count
}

// flyAcceptedNode ports FlyNodeEvaluator.findAcceptedNode: a block the mob
// may fly through, where flying right above the ground costs a little more.
func (w flyNodeEvaluator) flyAcceptedNode(x, y, z int) *pathNodeData {
	t := w.cachedPathType(x, y, z)
	cost := w.m.PathfindingMalus(t)
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

// flyPathType ports FlyNodeEvaluator.getPathType: open air takes on the
// danger of the block below it, and air right above the ground is walkable.
func flyPathType(m *Mob, pos cube.Pos) PathType {
	tx := m.tx
	t := pathTypeFromState(tx, pos)
	if t == PathOpen && pos.Y() >= tx.Range().Min()+1 {
		below := pos.Side(cube.FaceDown)
		switch belowType := pathTypeFromState(tx, below); belowType {
		case PathFire, PathLava:
			t = PathFire
		case PathDamaging:
			t = PathDamaging
		case PathCocoa:
			t = PathCocoa
		case PathFence:
			if below != m.BlockPos() {
				t = PathFence
			}
		case PathWalkable, PathOpen, PathWater:
		default:
			t = PathWalkable
		}
	}
	if t == PathWalkable || t == PathOpen {
		t = checkNeighbourBlocks(tx, pos, t)
	}
	return t
}
