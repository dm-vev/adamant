package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
)

var dragonNodeAdjacency = [24]uint32{
	6146, 8197, 8202, 16404, 32808, 32848, 65696, 131392,
	131712, 263424, 526848, 525313, 1581057, 3166214, 2138120, 6373424,
	4358208, 12910976, 9044480, 9706496, 15216640, 13688832, 11763712, 8257536,
}

// dragonPath ports the Path an EnderDragon follows between its nodes: the
// nodes from the start, a final node if one was given, and the index of the
// next node to fly to. A path that is not valid is Java's null path.
type dragonPath struct {
	nodes [25]cube.Pos
	n     int
	next  int
	valid bool
}

func (p *dragonPath) done() bool          { return p.next >= p.n }
func (p *dragonPath) advance()            { p.next++ }
func (p *dragonPath) nextPos() cube.Pos   { return p.nodes[p.next] }
func (p *dragonPath) finish()             { p.next = p.n }
func (p *dragonPath) live() bool          { return p.valid && !p.done() }
func (p *dragonPath) finished() bool      { return p.valid && p.done() }
func (p *dragonPath) missingOrDone() bool { return !p.valid || p.done() }

// ensureDragonNodes ports the first part of EnderDragon.findClosestNode: the
// 24 nodes in three rings above the island, built once.
func (b *EnderDragonBehaviour) ensureDragonNodes(tx *world.Tx) {
	if b.nodesInitialized {
		return
	}
	for i := range b.nodes {
		yAdjustment, multiplier := 5, i
		var x, z int
		switch {
		case i < 12:
			angle := float32(2) * (float32(-math.Pi) + float32(math.Pi/12)*float32(multiplier))
			x, z = int(math.Floor(float64(float32(60)*vanilla.Cos(float64(angle))))), int(math.Floor(float64(float32(60)*vanilla.Sin(float64(angle)))))
		case i < 20:
			multiplier -= 12
			angle := float32(2) * (float32(-math.Pi) + float32(math.Pi/8)*float32(multiplier))
			x, z = int(math.Floor(float64(float32(40)*vanilla.Cos(float64(angle))))), int(math.Floor(float64(float32(40)*vanilla.Sin(float64(angle)))))
			yAdjustment += 10
		default:
			multiplier -= 20
			angle := float32(2) * (float32(-math.Pi) + float32(math.Pi/4)*float32(multiplier))
			x, z = int(math.Floor(float64(float32(20)*vanilla.Cos(float64(angle))))), int(math.Floor(float64(float32(20)*vanilla.Sin(float64(angle)))))
		}
		y := max(73, tx.JavaMotionBlockingHeight(x, z, true)+yAdjustment)
		b.nodes[i] = cube.Pos{x, y, z}
	}
	b.nodesInitialized = true
}

// closestNode ports EnderDragon.findClosestNode().
func (b *EnderDragonBehaviour) closestNode(tx *world.Tx, e *Ent) int {
	b.ensureDragonNodes(tx)
	pos := e.Position()
	return b.closestNodeTo(tx, pos[0], pos[1], pos[2])
}

// closestNodeTo ports EnderDragon.findClosestNode(x, y, z): the outer ring is
// left out once no crystal is alive.
func (b *EnderDragonBehaviour) closestNodeTo(tx *world.Tx, x, y, z float64) int {
	b.ensureDragonNodes(tx)
	start := 0
	if b.fight == nil || b.fight.AliveCrystals() == 0 {
		start = 12
	}
	point := cube.Pos{int(math.Floor(x)), int(math.Floor(y)), int(math.Floor(z))}
	closest, distance := 0, float32(10000)
	for i := start; i < len(b.nodes); i++ {
		if d := dragonNodeDistanceSqr(b.nodes[i], point); d < distance {
			closest, distance = i, d
		}
	}
	return closest
}

func dragonNodeDistanceSqr(a, c cube.Pos) float32 {
	dx, dy, dz := float32(c[0]-a[0]), float32(c[1]-a[1]), float32(c[2]-a[2])
	return dx*dx + dy*dy + dz*dz
}

func dragonNodeDistance(a, c cube.Pos) float32 {
	return float32(math.Sqrt(float64(dragonNodeDistanceSqr(a, c))))
}

// dragonHeap ports the BinaryHeap the dragon's A* keeps its open nodes in, so
// that nodes of equal cost are taken in the same order.
type dragonHeap struct {
	heap [24]int
	size int
	idx  *[24]int
	f    *[24]float32
}

func (h *dragonHeap) insert(n int) {
	h.heap[h.size] = n
	h.idx[n] = h.size
	h.up(h.size)
	h.size++
}

func (h *dragonHeap) pop() int {
	popped := h.heap[0]
	h.size--
	h.heap[0] = h.heap[h.size]
	if h.size > 0 {
		h.down(0)
	}
	h.idx[popped] = -1
	return popped
}

func (h *dragonHeap) changeCost(n int, cost float32) {
	old := h.f[n]
	h.f[n] = cost
	if cost < old {
		h.up(h.idx[n])
	} else {
		h.down(h.idx[n])
	}
}

func (h *dragonHeap) up(i int) {
	n := h.heap[i]
	cost := h.f[n]
	for i > 0 {
		parent := (i - 1) >> 1
		p := h.heap[parent]
		if !(cost < h.f[p]) {
			break
		}
		h.heap[i] = p
		h.idx[p] = i
		i = parent
	}
	h.heap[i] = n
	h.idx[n] = i
}

func (h *dragonHeap) down(i int) {
	n := h.heap[i]
	cost := h.f[n]
	for {
		left := 1 + i<<1
		right := left + 1
		if left >= h.size {
			break
		}
		leftNode := h.heap[left]
		leftCost := h.f[leftNode]
		rightNode, rightCost := -1, float32(math.Inf(1))
		if right < h.size {
			rightNode = h.heap[right]
			rightCost = h.f[rightNode]
		}
		if leftCost < rightCost {
			if !(leftCost < cost) {
				break
			}
			h.heap[i] = leftNode
			h.idx[leftNode] = i
			i = left
		} else {
			if !(rightCost < cost) {
				break
			}
			h.heap[i] = rightNode
			h.idx[rightNode] = i
			i = right
		}
	}
	h.heap[i] = n
	h.idx[n] = i
}

// findPath ports EnderDragon.findPath: A* over the node graph, leaving out
// the outer ring once no crystal is alive. It ends at the final node if one
// is given, and at the closest node reached if the end cannot be.
func (b *EnderDragonBehaviour) findPath(tx *world.Tx, start, end int, final *cube.Pos) dragonPath {
	b.ensureDragonNodes(tx)
	var g, f [24]float32
	var idx [24]int
	var cameFrom [24]int
	var closed [24]bool
	for i := range idx {
		idx[i], cameFrom[i] = -1, -1
	}
	open := dragonHeap{idx: &idx, f: &f}
	from, to := b.nodes[start], b.nodes[end]
	f[start] = dragonNodeDistance(from, to)
	open.insert(start)
	closest := start
	minimum := 0
	if b.fight == nil || b.fight.AliveCrystals() == 0 {
		minimum = 12
	}
	reconstruct := func(last int, final *cube.Pos) dragonPath {
		var p dragonPath
		var reverse [24]int
		n := 0
		for node := last; node >= 0; node = cameFrom[node] {
			reverse[n] = node
			n++
		}
		for i := n - 1; i >= 0; i-- {
			p.nodes[p.n] = b.nodes[reverse[i]]
			p.n++
		}
		if final != nil {
			p.nodes[p.n] = *final
			p.n++
		}
		p.valid = true
		return p
	}
	for open.size > 0 {
		node := open.pop()
		// Node.equals compares positions, and two nodes may share one.
		if b.nodes[node] == to {
			return reconstruct(node, final)
		}
		if dragonNodeDistance(b.nodes[node], to) < dragonNodeDistance(b.nodes[closest], to) {
			closest = node
		}
		closed[node] = true
		for i := minimum; i < 24; i++ {
			if dragonNodeAdjacency[node]&(1<<i) == 0 || closed[i] {
				continue
			}
			tentative := g[node] + dragonNodeDistance(b.nodes[node], b.nodes[i])
			if idx[i] < 0 || tentative < g[i] {
				cameFrom[i] = node
				g[i] = tentative
				h := dragonNodeDistance(b.nodes[i], to)
				if idx[i] >= 0 {
					open.changeCost(i, g[i]+h)
				} else {
					f[i] = g[i] + h
					open.insert(i)
				}
			}
		}
	}
	if closest == start {
		return dragonPath{}
	}
	return reconstruct(closest, final)
}
