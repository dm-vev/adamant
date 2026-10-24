package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

// PathType is what a block is to a mob finding a path through it, ported
// from Java's PathType.
type PathType uint8

const (
	PathBlocked PathType = iota
	PathOpen
	PathWalkable
	PathWalkableDoor
	PathTrapdoor
	PathPowderSnow
	PathOnTopOfPowderSnow
	PathFence
	PathLava
	PathWater
	PathWaterBorder
	PathRail
	PathUnpassableRail
	PathFireInNeighbour
	PathFire
	PathDamagingInNeighbour
	PathDamaging
	PathDoorOpen
	PathDoorWoodClosed
	PathDoorIronClosed
	PathBreach
	PathLeaves
	PathStickyHoney
	PathCocoa
	PathDamageCautious
	PathOnTopOfTrapdoor
	PathBigMobsCloseToDanger
	pathTypeCount
)

// pathMaluses are the default maluses of the path types.
var pathMaluses = [pathTypeCount]float64{
	PathBlocked: -1, PathPowderSnow: -1, PathFence: -1, PathLava: -1, PathWater: 8, PathWaterBorder: 8,
	PathUnpassableRail: -1, PathFireInNeighbour: 8, PathFire: 16, PathDamagingInNeighbour: 8, PathDamaging: -1,
	PathDoorWoodClosed: -1, PathDoorIronClosed: -1, PathBreach: 4, PathLeaves: -1, PathStickyHoney: 8,
	PathBigMobsCloseToDanger: 4,
}

// Malus returns the cost a mob adds to a path through the type by default,
// negative where it never goes.
func (t PathType) Malus() float64 { return pathMaluses[t] }

// pathNodeKey packs the position of a node.
func pathNodeKey(x, y, z int) int64 {
	return int64(x&0x3ffffff)<<38 | int64(z&0x3ffffff)<<12 | int64(y&0xfff)
}

// pathNodeData is a node of a path, ported from Java's Node.
type pathNodeData struct {
	x, y, z        int
	heapIdx        int
	g, h, f        float32
	cameFrom       *pathNodeData
	closed         bool
	walkedDistance float32
	costMalus      float32
	typ            PathType
}

func newPathNode(x, y, z int) *pathNodeData {
	return &pathNodeData{x: x, y: y, z: z, heapIdx: -1}
}

// Pos returns the block position of the node.
func (n *pathNodeData) Pos() cube.Pos { return cube.Pos{n.x, n.y, n.z} }

func (n *pathNodeData) cloneAndMove(x, y, z int) *pathNodeData {
	c := *n
	c.x, c.y, c.z = x, y, z
	return &c
}

func (n *pathNodeData) distanceTo(o *pathNodeData) float32 {
	xd, yd, zd := float32(o.x-n.x), float32(o.y-n.y), float32(o.z-n.z)
	return float32(math.Sqrt(float64(xd*xd + yd*yd + zd*zd)))
}

func (n *pathNodeData) distanceManhattan(x, y, z int) float32 {
	return float32(absInt(x-n.x) + absInt(y-n.y) + absInt(z-n.z))
}

func (n *pathNodeData) inOpenSet() bool { return n.heapIdx >= 0 }

// pathHeapQueue is the open set of a path finder, ported from Java's
// BinaryHeap.
type pathHeapQueue struct {
	heap []*pathNodeData
}

func (h *pathHeapQueue) insert(n *pathNodeData) {
	h.heap = append(h.heap, n)
	n.heapIdx = len(h.heap) - 1
	h.upHeap(n.heapIdx)
}

func (h *pathHeapQueue) clear() {
	clear(h.heap)
	h.heap = h.heap[:0]
}

func (h *pathHeapQueue) pop() *pathNodeData {
	popped := h.heap[0]
	last := len(h.heap) - 1
	h.heap[0] = h.heap[last]
	h.heap[last] = nil
	h.heap = h.heap[:last]
	if len(h.heap) > 0 {
		h.downHeap(0)
	}
	popped.heapIdx = -1
	return popped
}

func (h *pathHeapQueue) changeCost(n *pathNodeData, cost float32) {
	old := n.f
	n.f = cost
	if cost < old {
		h.upHeap(n.heapIdx)
	} else {
		h.downHeap(n.heapIdx)
	}
}

func (h *pathHeapQueue) upHeap(idx int) {
	n := h.heap[idx]
	for idx > 0 {
		parentIdx := (idx - 1) >> 1
		parent := h.heap[parentIdx]
		if !(n.f < parent.f) {
			break
		}
		h.heap[idx] = parent
		parent.heapIdx = idx
		idx = parentIdx
	}
	h.heap[idx] = n
	n.heapIdx = idx
}

func (h *pathHeapQueue) downHeap(idx int) {
	n := h.heap[idx]
	size := len(h.heap)
	for {
		left := 1 + idx<<1
		right := left + 1
		if left >= size {
			break
		}
		leftNode := h.heap[left]
		var rightNode *pathNodeData
		rightCost := float32(math.Inf(1))
		if right < size {
			rightNode = h.heap[right]
			rightCost = rightNode.f
		}
		if leftNode.f < rightCost {
			if !(leftNode.f < n.f) {
				break
			}
			h.heap[idx] = leftNode
			leftNode.heapIdx = idx
			idx = left
		} else {
			if !(rightCost < n.f) {
				break
			}
			h.heap[idx] = rightNode
			rightNode.heapIdx = idx
			idx = right
		}
	}
	h.heap[idx] = n
	n.heapIdx = idx
}

// mobPath is a path a mob follows, ported from Java's Path.
type mobPath struct {
	nodes        []*pathNodeData
	next         int
	target       cube.Pos
	distToTarget float32
	reached      bool
}

func newMobPath(nodes []*pathNodeData, target cube.Pos, reached bool) *mobPath {
	p := &mobPath{nodes: nodes, target: target, reached: reached, distToTarget: math.MaxFloat32}
	if len(nodes) > 0 {
		p.distToTarget = nodes[len(nodes)-1].distanceManhattan(target.X(), target.Y(), target.Z())
	}
	return p
}

// Advance moves on to the next node of the path.
func (p *mobPath) Advance() { p.next++ }

// Done reports if the mob walked the whole path.
func (p *mobPath) Done() bool { return p.next >= len(p.nodes) }

// NodeCount returns the number of nodes of the path.
func (p *mobPath) NodeCount() int { return len(p.nodes) }

// EndNode returns the last node of the path, or nil.
func (p *mobPath) EndNode() *pathNodeData {
	if len(p.nodes) == 0 {
		return nil
	}
	return p.nodes[len(p.nodes)-1]
}

// CanReach reports if the path reaches its target.
func (p *mobPath) CanReach() bool { return p.reached }

// Target returns the position the path was found towards.
func (p *mobPath) Target() cube.Pos { return p.target }

func (p *mobPath) nextNodePos() cube.Pos { return p.nodes[p.next].Pos() }

func (p *mobPath) nodePos(i int) cube.Pos { return p.nodes[i].Pos() }

// entityPosAtNode returns where the mob stands at node i, ported from
// Path.getEntityPosAtNode.
func (p *mobPath) entityPosAtNode(width float64, i int) mgl64.Vec3 {
	n := p.nodes[i]
	offset := float64(int(width+1)) * .5
	return mgl64.Vec3{float64(n.x) + offset, float64(n.y), float64(n.z) + offset}
}

func (p *mobPath) truncate(index int) {
	if len(p.nodes) > index {
		clear(p.nodes[index:])
		p.nodes = p.nodes[:index]
	}
}

func (p *mobPath) sameAs(o *mobPath) bool {
	if o == nil || len(o.nodes) != len(p.nodes) {
		return false
	}
	for i, n := range p.nodes {
		if m := o.nodes[i]; m.x != n.x || m.y != n.y || m.z != n.z {
			return false
		}
	}
	return true
}

// pathTarget is a target of a path finder, ported from Java's Target.
type pathTarget struct {
	pos           cube.Pos
	bestHeuristic float32
	bestNode      *pathNodeData
	reached       bool
}

func (t *pathTarget) updateBest(h float32, n *pathNodeData) {
	if h < t.bestHeuristic {
		t.bestHeuristic, t.bestNode = h, n
	}
}

// findPath finds a path from the mob to one of the targets with the A*
// search of Java's PathFinder.findPath.
// nodeEvaluator finds the nodes a mob may move to, as Java's NodeEvaluator.
type nodeEvaluator interface {
	prepare(m *Mob)
	done()
	start() *pathNodeData
	neighbours(out []*pathNodeData, pos *pathNodeData) int
}

func findPath(m *Mob, eval nodeEvaluator, targets []cube.Pos, maxPathLength float32, reachRange int, maxVisitedNodes int) *mobPath {
	eval.prepare(m)
	defer eval.done()
	from := eval.start()
	if from == nil {
		return nil
	}
	ts := make([]*pathTarget, len(targets))
	for i, pos := range targets {
		ts[i] = &pathTarget{pos: pos, bestHeuristic: math.MaxFloat32}
	}
	bestH := func(n *pathNodeData) float32 {
		best := float32(math.MaxFloat32)
		for _, t := range ts {
			xd, yd, zd := float32(t.pos.X()-n.x), float32(t.pos.Y()-n.y), float32(t.pos.Z()-n.z)
			h := float32(math.Sqrt(float64(xd*xd + yd*yd + zd*zd)))
			t.updateBest(h, n)
			best = min(best, h)
		}
		return best
	}
	from.g = 0
	from.h = bestH(from)
	from.f = from.h
	var open pathHeapQueue
	open.insert(from)
	reached := false
	count := 0
	var neighbours [32]*pathNodeData
	for len(open.heap) > 0 {
		if count++; count >= maxVisitedNodes {
			break
		}
		current := open.pop()
		current.closed = true
		for _, t := range ts {
			if current.distanceManhattan(t.pos.X(), t.pos.Y(), t.pos.Z()) <= float32(reachRange) {
				t.reached, reached = true, true
			}
		}
		if reached {
			break
		}
		if current.distanceTo(from) >= maxPathLength {
			continue
		}
		count := eval.neighbours(neighbours[:], current)
		for i := range count {
			n := neighbours[i]
			distance := current.distanceTo(n)
			n.walkedDistance = current.walkedDistance + distance
			tentative := current.g + distance + n.costMalus
			if n.walkedDistance < maxPathLength && (!n.inOpenSet() || tentative < n.g) {
				n.cameFrom = current
				n.g = tentative
				n.h = bestH(n) * 1.5
				if n.inOpenSet() {
					open.changeCost(n, n.g+n.h)
				} else {
					n.f = n.g + n.h
					open.insert(n)
				}
			}
		}
	}
	var best *mobPath
	for _, t := range ts {
		if reached && !t.reached || t.bestNode == nil {
			continue
		}
		p := reconstructMobPath(t.bestNode, t.pos, t.reached)
		switch {
		case best == nil:
			best = p
		case reached && p.NodeCount() < best.NodeCount():
			best = p
		case !reached && (p.distToTarget < best.distToTarget || p.distToTarget == best.distToTarget && p.NodeCount() < best.NodeCount()):
			best = p
		}
	}
	return best
}

func reconstructMobPath(closest *pathNodeData, target cube.Pos, reached bool) *mobPath {
	var nodes []*pathNodeData
	for n := closest; n != nil; n = n.cameFrom {
		nodes = append(nodes, n)
	}
	for i, j := 0, len(nodes)-1; i < j; i, j = i+1, j-1 {
		nodes[i], nodes[j] = nodes[j], nodes[i]
	}
	return newMobPath(nodes, target, reached)
}
