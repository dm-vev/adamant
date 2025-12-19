package entity

import (
	"container/heap"
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

type pathNode struct {
	pos    cube.Pos
	g, f   float64
	parent int
	index  int
}

type pathHeap []*pathNode

func (h pathHeap) Len() int           { return len(h) }
func (h pathHeap) Less(i, j int) bool { return h[i].f < h[j].f }
func (h pathHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}
func (h *pathHeap) Push(x any) {
	n := x.(*pathNode)
	n.index = len(*h)
	*h = append(*h, n)
}
func (h *pathHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]
	return item
}

func findPath(tx *world.Tx, start, goal cube.Pos, maxNodes int, allowWater bool) []cube.Pos {
	if start == goal {
		return nil
	}

	open := pathHeap{}
	heap.Init(&open)

	nodes := make([]pathNode, 0, maxNodes)
	index := make(map[cube.Pos]int, maxNodes)

	startNode := pathNode{pos: start, g: 0, f: heuristic(start, goal), parent: -1}
	nodes = append(nodes, startNode)
	index[start] = 0
	heap.Push(&open, &nodes[0])

	closed := make(map[cube.Pos]struct{}, maxNodes)

	for open.Len() > 0 && len(nodes) < maxNodes {
		current := heap.Pop(&open).(*pathNode)
		if current.pos == goal {
			return reconstructPath(nodes, current)
		}
		if _, ok := closed[current.pos]; ok {
			continue
		}
		closed[current.pos] = struct{}{}

		for _, dir := range []cube.Face{cube.FaceNorth, cube.FaceSouth, cube.FaceEast, cube.FaceWest} {
			next, ok := nextStep(tx, current.pos, dir, allowWater)
			if !ok {
				continue
			}
			if _, ok := closed[next]; ok {
				continue
			}
			g := current.g + stepCost(tx, next, allowWater)
			if idx, ok := index[next]; ok {
				if g < nodes[idx].g {
					nodes[idx].g = g
					nodes[idx].f = g + heuristic(next, goal)
					nodes[idx].parent = index[current.pos]
					heap.Push(&open, &nodes[idx])
				}
				continue
			}
			node := pathNode{
				pos:    next,
				g:      g,
				f:      g + heuristic(next, goal),
				parent: index[current.pos],
			}
			nodes = append(nodes, node)
			index[next] = len(nodes) - 1
			heap.Push(&open, &nodes[len(nodes)-1])
		}
	}
	return nil
}

func reconstructPath(nodes []pathNode, current *pathNode) []cube.Pos {
	path := make([]cube.Pos, 0, 8)
	for current.parent != -1 {
		path = append(path, current.pos)
		current = &nodes[current.parent]
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

func heuristic(a, b cube.Pos) float64 {
	dx := float64(a.X() - b.X())
	dy := float64(a.Y() - b.Y())
	dz := float64(a.Z() - b.Z())
	return math.Abs(dx) + math.Abs(dy) + math.Abs(dz)
}

func stepCost(tx *world.Tx, pos cube.Pos, allowWater bool) float64 {
	cost := 1.0
	if l, ok := tx.Liquid(pos); ok {
		switch l.(type) {
		case block.Water:
			if allowWater {
				cost += 4
			} else {
				cost += 100
			}
		case block.Lava:
			cost += 10
		default:
			cost += 6
		}
	}
	if _, ok := tx.Block(pos).(block.Fire); ok {
		cost += 8
	}
	return cost
}

func nextStep(tx *world.Tx, current cube.Pos, dir cube.Face, allowWater bool) (cube.Pos, bool) {
	next := current.Side(dir)
	if walkable(tx, next, allowWater) {
		return next, true
	}
	up := next.Side(cube.FaceUp)
	if walkable(tx, up, allowWater) && solidBelow(tx, up) {
		return up, true
	}
	down := next.Side(cube.FaceDown)
	if walkable(tx, down, allowWater) && solidBelow(tx, down) {
		return down, true
	}
	return cube.Pos{}, false
}

func walkable(tx *world.Tx, pos cube.Pos, allowWater bool) bool {
	if pos.OutOfBounds(tx.Range()) {
		return false
	}
	if !allowWater {
		if _, ok := tx.Liquid(pos); ok {
			return false
		}
	}
	if !isPassable(tx, pos) {
		return false
	}
	above := pos.Side(cube.FaceUp)
	if !isPassable(tx, above) {
		return false
	}
	return solidBelow(tx, pos)
}

func solidBelow(tx *world.Tx, pos cube.Pos) bool {
	below := pos.Side(cube.FaceDown)
	return tx.Block(below).Model().FaceSolid(below, cube.FaceUp, tx)
}

func isPassable(tx *world.Tx, pos cube.Pos) bool {
	b := tx.Block(pos)
	return len(b.Model().BBox(pos, tx)) == 0
}
