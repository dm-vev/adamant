package entity

import (
	"math"

	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/world"
)

// swimNodeEvaluator finds the nodes a mob swims to through water, ported
// from Java's SwimNodeEvaluator.
type swimNodeEvaluator struct {
	// allowBreaching lets the mob leap out of the water, as dolphins do.
	allowBreaching bool

	m                                      *Mob
	scratch                                *pathSearchScratch
	minY, maxY                             int
	entityWidth, entityHeight, entityDepth int
}

func (w *swimNodeEvaluator) pathWorld(m *Mob) pathWorld { return m.tx }

func (w *swimNodeEvaluator) prepare(m *Mob, scratch *pathSearchScratch, _ *pathTypeCache) {
	w.m = m
	w.scratch = scratch
	scratch.malus = pathMaluses
	for t, malus := range m.AI.maluses {
		scratch.malus[t] = malus
	}
	scratch.nodes.reset()
	scratch.types.reset()
	rng := m.tx.Range()
	w.minY, w.maxY = rng.Min(), rng.Max()
	bb := m.BBox()
	w.entityWidth = int(math.Floor(bb.Width() + 1))
	w.entityHeight = int(math.Floor(bb.Height() + 1))
	w.entityDepth = w.entityWidth
}

func (w *swimNodeEvaluator) done() { w.m, w.scratch = nil, nil }

func (w *swimNodeEvaluator) node(x, y, z int) *pathNodeData {
	return w.scratch.nodes.getOrAlloc(x, y, z, &w.scratch.arena)
}

func (w *swimNodeEvaluator) blockAt(pos cube.Pos) world.Block {
	if pos[1] < w.minY || pos[1] > w.maxY {
		return block.Air{}
	}
	b, ok := w.m.tx.BlockLoaded(pos)
	if !ok {
		return block.Air{}
	}
	return b
}

// start ports SwimNodeEvaluator.getStart.
func (w *swimNodeEvaluator) start() *pathNodeData {
	bb := w.m.BBox()
	return w.node(int(math.Floor(bb.Min()[0])), int(math.Floor(bb.Min()[1]+.5)), int(math.Floor(bb.Min()[2])))
}

// swimDirections are Java's Direction.values() in order: down, up, north,
// south, west and east.
var swimDirections = [6]cube.Pos{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

// swimHorizontal are Direction.Plane.HORIZONTAL indices into swimDirections:
// north, east, south, west.
var swimHorizontal = [4]int{2, 5, 3, 4}

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
	for i, d := range swimHorizontal {
		second := swimHorizontal[(i+1)&3]
		na, nb := around[d], around[second]
		if na == nil || na.costMalus < 0 || nb == nil || nb.costMalus < 0 {
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
	if (!w.allowBreaching || t != PathBreach) && t != PathWater {
		return nil
	}
	cost := w.scratch.malus[t]
	if cost < 0 {
		return nil
	}
	n := w.node(x, y, z)
	n.typ = t
	n.costMalus = max(n.costMalus, float32(cost))
	if t == PathBreach {
		// PathWater guarantees water at (x, y, z); only PathBreach may lack liquid there.
		if _, liquid := w.m.tx.Liquid(cube.Pos{x, y, z}); !liquid {
			n.costMalus += 8
		}
	}
	return n
}

func (w *swimNodeEvaluator) cachedType(x, y, z int) PathType {
	key := pathNodeKey(x, y, z)
	if t, ok := w.scratch.types.get(key); ok {
		return t
	}
	t := w.pathTypeOfMob(x, y, z)
	w.scratch.types.put(key, t)
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
				l, liquid := tx.Liquid(last)
				if !liquid {
					if pathfindableWater(w.m, cube.Pos{xx, yy - 1, zz}) && isAir(w.blockAt(last)) {
						return PathBreach
					}
					return PathBlocked
				}
				if l.LiquidType() != "water" {
					return PathBlocked
				}
			}
		}
	}
	// Every cell in the box including last is confirmed water; only collision clearance remains.
	if len(w.blockAt(last).Model().BBox(last, tx)) == 0 {
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
