package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// swimNodeEvaluator finds the nodes a mob swims to through water, ported
// from Java's SwimNodeEvaluator.
type swimNodeEvaluator struct {
	// allowBreaching lets the mob leap out of the water, as dolphins do.
	allowBreaching bool

	m                                      *Mob
	nodes                                  map[int64]*pathNodeData
	types                                  map[int64]PathType
	entityWidth, entityHeight, entityDepth int
}

func (w *swimNodeEvaluator) prepare(m *Mob) {
	w.m = m
	w.nodes = make(map[int64]*pathNodeData, 128)
	w.types = make(map[int64]PathType, 256)
	w.entityWidth = int(math.Floor(m.Width() + 1))
	w.entityHeight = int(math.Floor(m.Height() + 1))
	w.entityDepth = w.entityWidth
}

func (w *swimNodeEvaluator) done() { w.m, w.nodes, w.types = nil, nil, nil }

func (w *swimNodeEvaluator) node(x, y, z int) *pathNodeData {
	key := pathNodeKey(x, y, z)
	if n, ok := w.nodes[key]; ok {
		return n
	}
	n := newPathNode(x, y, z)
	w.nodes[key] = n
	return n
}

// start ports SwimNodeEvaluator.getStart.
func (w *swimNodeEvaluator) start() *pathNodeData {
	bb := w.m.BBox()
	return w.node(int(math.Floor(bb.Min()[0])), int(math.Floor(bb.Min()[1]+.5)), int(math.Floor(bb.Min()[2])))
}

// swimDirections are Java's Direction.values() in order: down, up, north,
// south, west and east.
var swimDirections = [6]cube.Pos{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

// neighbours ports SwimNodeEvaluator.getNeighbors.
func (w *swimNodeEvaluator) neighbours(out []*pathNodeData, pos *pathNodeData) int {
	count := 0
	var around [6]*pathNodeData
	for i, d := range swimDirections {
		n := w.findAcceptedNode(pos.x+d[0], pos.y+d[1], pos.z+d[2])
		around[i] = n
		if n != nil && !n.closed {
			out[count] = n
			count++
		}
	}
	hasMalus := func(n *pathNodeData) bool { return n != nil && n.costMalus >= 0 }
	// Direction.Plane.HORIZONTAL: north, east, south, west, each with the
	// direction clockwise of it.
	horizontal := [4]int{2, 5, 3, 4}
	for i, d := range horizontal {
		second := horizontal[(i+1)%4]
		if !hasMalus(around[d]) || !hasMalus(around[second]) {
			continue
		}
		a, b := swimDirections[d], swimDirections[second]
		n := w.findAcceptedNode(pos.x+a[0]+b[0], pos.y, pos.z+a[2]+b[2])
		if n != nil && !n.closed {
			out[count] = n
			count++
		}
	}
	return count
}

// findAcceptedNode ports SwimNodeEvaluator.findAcceptedNode: water, or the
// air a breaching mob leaps through at a cost.
func (w *swimNodeEvaluator) findAcceptedNode(x, y, z int) *pathNodeData {
	t := w.cachedType(x, y, z)
	if !(w.allowBreaching && t == PathBreach || t == PathWater) {
		return nil
	}
	cost := w.m.PathfindingMalus(t)
	if cost < 0 {
		return nil
	}
	n := w.node(x, y, z)
	n.typ = t
	n.costMalus = max(n.costMalus, float32(cost))
	if _, liquid := w.m.tx.Liquid(cube.Pos{x, y, z}); !liquid {
		n.costMalus += 8
	}
	return n
}

func (w *swimNodeEvaluator) cachedType(x, y, z int) PathType {
	key := pathNodeKey(x, y, z)
	if t, ok := w.types[key]; ok {
		return t
	}
	t := w.pathTypeOfMob(x, y, z)
	w.types[key] = t
	return t
}

// pathTypeOfMob ports SwimNodeEvaluator.getPathTypeOfMob: water the whole
// mob fits in, the air above water a mob may breach into, or blocked.
func (w *swimNodeEvaluator) pathTypeOfMob(x, y, z int) PathType {
	tx := w.m.tx
	var last cube.Pos
	for xx := x; xx < x+w.entityWidth; xx++ {
		for yy := y; yy < y+w.entityHeight; yy++ {
			for zz := z; zz < z+w.entityDepth; zz++ {
				last = cube.Pos{xx, yy, zz}
				_, liquid := tx.Liquid(last)
				if !liquid && pathfindableWater(w.m, last.Side(cube.FaceDown)) && isAir(pathBlockAt(tx, last)) {
					return PathBreach
				}
				if !pathWater(tx, last) {
					return PathBlocked
				}
			}
		}
	}
	if pathfindableWater(w.m, last) {
		return PathWater
	}
	return PathBlocked
}

// pathfindableWater ports BlockState.isPathfindable for
// PathComputationType.WATER: water a mob swims through without bumping into
// a block.
func pathfindableWater(m *Mob, pos cube.Pos) bool {
	if !pathWater(m.tx, pos) {
		return false
	}
	return len(pathBlockAt(m.tx, pos).Model().BBox(pos, m.tx)) == 0
}
