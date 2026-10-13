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
	parent *pathNode
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
	item.index = -1
	*h = old[:n-1]
	return item
}

// FindGroundPath finds a route using Java-like eight-way ground navigation.
func FindGroundPath(tx *world.Tx, start, goal cube.Pos, profile GroundProfile) []cube.Pos {
	if start == goal {
		return nil
	}
	if profile.StepHeight < 0 {
		profile.StepHeight = 0
	}
	if profile.StepHeight == 0 {
		profile.StepHeight = 1
	}
	if profile.MaxFall <= 0 {
		profile.MaxFall = 3
	}
	if profile.MaxVisitedNodes <= 0 {
		if profile.FollowRange > 0 {
			profile.MaxVisitedNodes = int(math.Floor(profile.FollowRange * 16))
		}
		if profile.MaxVisitedNodes <= 0 {
			profile.MaxVisitedNodes = 256
		}
	}
	open := pathHeap{}
	heap.Init(&open)
	index := make(map[cube.Pos]*pathNode, profile.MaxVisitedNodes)
	closed := make(map[cube.Pos]struct{}, profile.MaxVisitedNodes)
	n := &pathNode{pos: start, f: heuristic(start, goal)}
	index[start] = n
	heap.Push(&open, n)
	expanded := 0
	for open.Len() > 0 && expanded < profile.MaxVisitedNodes {
		current := heap.Pop(&open).(*pathNode)
		if _, seen := closed[current.pos]; seen {
			continue
		}
		closed[current.pos] = struct{}{}
		expanded++
		if current.pos == goal {
			return reconstructPath(current)
		}
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				if dx == 0 && dz == 0 {
					continue
				}
				for dy := profile.StepHeight; dy >= -profile.MaxFall; dy-- {
					next := current.pos.Add(cube.Pos{dx, dy, dz})
					if !groundWalkable(tx, next, profile) {
						continue
					}
					// Java diagonal movement requires both orthogonal sides to be
					// traversable; checking them at the destination height prevents corner clipping.
					if dx != 0 && dz != 0 {
						if !groundWalkable(tx, current.pos.Add(cube.Pos{dx, dy, 0}), profile) ||
							!groundWalkable(tx, current.pos.Add(cube.Pos{0, dy, dz}), profile) {
							continue
						}
					}
					if profile.Allowed != nil && !profile.Allowed(next) {
						continue
					}
					if _, seen := closed[next]; seen {
						continue
					}
					cost := groundStepCost(tx, next, profile)*math.Hypot(float64(dx), float64(dz)) + math.Abs(float64(dy))
					g := current.g + cost
					if old := index[next]; old != nil {
						if g < old.g {
							old.g, old.parent, old.f = g, current, g+heuristic(next, goal)
							heap.Fix(&open, old.index)
						}
						continue
					}
					node := &pathNode{pos: next, g: g, f: g + heuristic(next, goal), parent: current}
					index[next] = node
					heap.Push(&open, node)
				}
			}
		}
	}
	return nil
}

func reconstructPath(current *pathNode) []cube.Pos {
	path := make([]cube.Pos, 0, 8)
	for current != nil && current.parent != nil {
		path = append(path, current.pos)
		current = current.parent
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

func heuristic(a, b cube.Pos) float64 {
	dx, dy, dz := float64(a.X()-b.X()), float64(a.Y()-b.Y()), float64(a.Z()-b.Z())
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

func groundStepCost(tx *world.Tx, pos cube.Pos, p GroundProfile) float64 {
	cost := 1.0
	if l, ok := tx.Liquid(pos); ok {
		switch l.(type) {
		case block.Water:
			if p.AllowWater {
				cost += 8
			} else {
				return 1e6
			}
		case block.Lava:
			return 1e6
		default:
			cost += 8
		}
	}
	switch b := tx.Block(pos).(type) {
	case block.Fire:
		cost += 16
	case block.WoodDoor:
		if !b.Open && !p.AllowDoors {
			return 1e6
		}
	case block.CopperDoor:
		if !b.Open && !p.AllowDoors {
			return 1e6
		}
	}
	return cost
}

func groundWalkable(tx *world.Tx, pos cube.Pos, p GroundProfile) bool {
	if pos.OutOfBounds(tx.Range()) {
		return false
	}
	if liquid, ok := tx.Liquid(pos); ok {
		switch liquid.(type) {
		case block.Lava:
			return false
		case block.Water:
			if !p.AllowWater {
				return false
			}
		default:
			if !p.AllowWater {
				return false
			}
		}
	}
	if !p.AllowDoors {
		switch b := tx.Block(pos).(type) {
		case block.WoodDoor:
			if !b.Open {
				return false
			}
		case block.CopperDoor:
			if !b.Open {
				return false
			}
		}
	}
	width := p.Width
	if width <= 0 {
		width = .6
	}
	height := p.Height
	if height <= 0 {
		height = 1.8
	}
	radius := int(math.Floor(width / 2))
	vertical := int(math.Ceil(height))
	for dx := -radius; dx <= radius; dx++ {
		for dz := -radius; dz <= radius; dz++ {
			for dy := range vertical {
				clearance := pos.Add(cube.Pos{dx, dy, dz})
				if clearance.OutOfBounds(tx.Range()) || !isPassable(tx, clearance) {
					return false
				}
			}
		}
	}
	return solidBelow(tx, pos)
}

func solidBelow(tx *world.Tx, pos cube.Pos) bool {
	below := pos.Side(cube.FaceDown)
	return len(tx.Block(below).Model().BBox(below, tx)) > 0
}
func isPassable(tx *world.Tx, pos cube.Pos) bool {
	return len(tx.Block(pos).Model().BBox(pos, tx)) == 0
}
