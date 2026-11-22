package entity

import (
	"math"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

type pathNode struct {
	pos    cube.Pos
	g, f   float64
	parent *pathNode
	index  int
	closed bool
}

const groundNodeSlabSize = 64

type groundNodeArena struct {
	slabs   []*[groundNodeSlabSize]pathNode
	slabIdx int
	slotIdx int
}

func (a *groundNodeArena) reset() {
	a.slabIdx, a.slotIdx = 0, 0
}

func (a *groundNodeArena) alloc(pos cube.Pos, g, f float64, parent *pathNode) *pathNode {
	if a.slotIdx == groundNodeSlabSize {
		a.slabIdx++
		a.slotIdx = 0
	}
	if a.slabIdx >= len(a.slabs) {
		a.slabs = append(a.slabs, new([groundNodeSlabSize]pathNode))
	}
	n := &a.slabs[a.slabIdx][a.slotIdx]
	a.slotIdx++
	*n = pathNode{pos: pos, g: g, f: f, parent: parent, index: -1}
	return n
}

func groundPosHash(p cube.Pos) uint64 {
	h := uint64(p[0])*0x9e3779b97f4a7c15 ^ uint64(p[2])*0xbf58476d1ce4e5b9 ^ uint64(p[1])*0x94d049bb133111eb
	return h ^ (h >> 32)
}

type groundNodeEntry struct {
	pos  cube.Pos
	node *pathNode
	gen  uint32
}

type groundNodeTable struct {
	entries []groundNodeEntry
	mask    uint64
	count   int
	gen     uint32
}

func (t *groundNodeTable) reset() {
	if len(t.entries) == 0 {
		t.entries = make([]groundNodeEntry, 256)
		t.mask = 255
	}
	t.count = 0
	t.gen++
	if t.gen == 0 {
		clear(t.entries)
		t.gen = 1
	}
}

func (t *groundNodeTable) grow() {
	old := t.entries
	newCap := len(old) * 2
	t.entries = make([]groundNodeEntry, newCap)
	t.mask = uint64(newCap - 1)
	gen := t.gen
	for i := range old {
		if old[i].gen != gen {
			continue
		}
		idx := groundPosHash(old[i].pos) & t.mask
		for t.entries[idx].gen == gen {
			idx = (idx + 1) & t.mask
		}
		t.entries[idx] = old[i]
	}
}

func (t *groundNodeTable) get(pos cube.Pos) *pathNode {
	idx := groundPosHash(pos) & t.mask
	gen := t.gen
	for {
		e := &t.entries[idx]
		if e.gen != gen {
			return nil
		}
		if e.pos == pos {
			return e.node
		}
		idx = (idx + 1) & t.mask
	}
}

func (t *groundNodeTable) put(pos cube.Pos, n *pathNode) {
	if (t.count+1)*2 >= len(t.entries) {
		t.grow()
	}
	idx := groundPosHash(pos) & t.mask
	gen := t.gen
	for {
		e := &t.entries[idx]
		if e.gen != gen {
			e.pos, e.node, e.gen = pos, n, gen
			t.count++
			return
		}
		if e.pos == pos {
			e.node = n
			return
		}
		idx = (idx + 1) & t.mask
	}
}

type groundWalkEntry struct {
	pos      cube.Pos
	cost     float64
	gen      uint32
	walkable bool
}

type groundWalkTable struct {
	entries []groundWalkEntry
	mask    uint64
	count   int
	gen     uint32
}

func (t *groundWalkTable) reset() {
	if len(t.entries) == 0 {
		t.entries = make([]groundWalkEntry, 512)
		t.mask = 511
	}
	t.count = 0
	t.gen++
	if t.gen == 0 {
		clear(t.entries)
		t.gen = 1
	}
}

func (t *groundWalkTable) grow() {
	old := t.entries
	newCap := len(old) * 2
	t.entries = make([]groundWalkEntry, newCap)
	t.mask = uint64(newCap - 1)
	gen := t.gen
	for i := range old {
		if old[i].gen != gen {
			continue
		}
		idx := groundPosHash(old[i].pos) & t.mask
		for t.entries[idx].gen == gen {
			idx = (idx + 1) & t.mask
		}
		t.entries[idx] = old[i]
	}
}

func (t *groundWalkTable) eval(tx *world.Tx, pos cube.Pos, p GroundProfile, rng cube.Range, radius, vertical int) (bool, float64) {
	if (t.count+1)*2 >= len(t.entries) {
		t.grow()
	}
	idx := groundPosHash(pos) & t.mask
	gen := t.gen
	for {
		e := &t.entries[idx]
		if e.gen != gen {
			ok, cost := groundWalkableAndCost(tx, pos, p, rng, radius, vertical)
			e.pos, e.walkable, e.cost, e.gen = pos, ok, cost, gen
			t.count++
			return ok, cost
		}
		if e.pos == pos {
			return e.walkable, e.cost
		}
		idx = (idx + 1) & t.mask
	}
}

type groundHeap []*pathNode

func (h *groundHeap) reset() {
	if len(*h) > 0 {
		clear(*h)
		*h = (*h)[:0]
	}
}

func (h *groundHeap) push(n *pathNode) {
	n.index = len(*h)
	*h = append(*h, n)
	h.up(n.index)
}

func (h *groundHeap) pop() *pathNode {
	s := *h
	n := len(s) - 1
	top := s[0]
	s[0] = s[n]
	s[0].index = 0
	s[n] = nil
	*h = s[:n]
	top.index = -1
	if n > 0 {
		h.down(0)
	}
	return top
}

func (h *groundHeap) fix(i int) {
	if !h.down(i) {
		h.up(i)
	}
}

func (h *groundHeap) up(j int) {
	s := *h
	for {
		i := (j - 1) / 2
		if i == j || !(s[j].f < s[i].f) {
			break
		}
		s[i], s[j] = s[j], s[i]
		s[i].index, s[j].index = i, j
		j = i
	}
}

func (h *groundHeap) down(i0 int) bool {
	s := *h
	n := len(s)
	i := i0
	for {
		j1 := 2*i + 1
		if j1 >= n || j1 < 0 {
			break
		}
		j := j1
		if j2 := j1 + 1; j2 < n && s[j2].f < s[j1].f {
			j = j2
		}
		if !(s[j].f < s[i].f) {
			break
		}
		s[i], s[j] = s[j], s[i]
		s[i].index, s[j].index = i, j
		i = j
	}
	return i > i0
}

type groundPathScratch struct {
	inUse    bool
	fromPool bool
	open     groundHeap
	arena    groundNodeArena
	nodes    groundNodeTable
	walkable groundWalkTable
}

var groundPathScratchPool = sync.Pool{
	New: func() any { return &groundPathScratch{} },
}

func acquireGroundScratch(tx *world.Tx) *groundPathScratch {
	if local := worldLocalFor(tx); local != nil && !local.ground.inUse {
		local.ground.inUse = true
		return &local.ground
	}
	s := groundPathScratchPool.Get().(*groundPathScratch)
	s.inUse, s.fromPool = true, true
	return s
}

func releaseGroundScratch(s *groundPathScratch) {
	s.open.reset()
	s.arena.reset()
	s.inUse = false
	if s.fromPool {
		s.fromPool = false
		groundPathScratchPool.Put(s)
	}
}

func normalizeGroundProfile(profile GroundProfile) (GroundProfile, int, int) {
	if profile.StepHeight <= 0 {
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
	width := profile.Width
	if width <= 0 {
		width = .6
	}
	height := profile.Height
	if height <= 0 {
		height = 1.8
	}
	return profile, int(math.Floor(width / 2)), int(math.Ceil(height))
}

// FindGroundPath finds a route using Java-like eight-way ground navigation.
func FindGroundPath(tx *world.Tx, start, goal cube.Pos, profile GroundProfile) []cube.Pos {
	if start == goal {
		return nil
	}
	profile, radius, vertical := normalizeGroundProfile(profile)
	rng := tx.Range()

	s := acquireGroundScratch(tx)
	defer releaseGroundScratch(s)
	s.open.reset()
	s.nodes.reset()
	s.walkable.reset()

	startNode := s.arena.alloc(start, 0, heuristic(start, goal), nil)
	s.nodes.put(start, startNode)
	s.open.push(startNode)
	expanded := 0
	for len(s.open) > 0 && expanded < profile.MaxVisitedNodes {
		current := s.open.pop()
		if current.closed {
			continue
		}
		current.closed = true
		expanded++
		if current.pos == goal {
			return reconstructPath(current)
		}
		cx, cy, cz := current.pos[0], current.pos[1], current.pos[2]
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				if dx == 0 && dz == 0 {
					continue
				}
				horiz := 1.0
				if dx != 0 && dz != 0 {
					horiz = math.Sqrt2
				}
				for dy := profile.StepHeight; dy >= -profile.MaxFall; dy-- {
					next := cube.Pos{cx + dx, cy + dy, cz + dz}
					ok, stepCost := s.walkable.eval(tx, next, profile, rng, radius, vertical)
					if !ok {
						continue
					}
					// Java diagonal movement requires both orthogonal sides to be
					// traversable; checking them at the destination height prevents corner clipping.
					if dx != 0 && dz != 0 {
						okX, _ := s.walkable.eval(tx, cube.Pos{cx + dx, cy + dy, cz}, profile, rng, radius, vertical)
						if !okX {
							continue
						}
						okZ, _ := s.walkable.eval(tx, cube.Pos{cx, cy + dy, cz + dz}, profile, rng, radius, vertical)
						if !okZ {
							continue
						}
					}
					if profile.Allowed != nil && !profile.Allowed(next) {
						continue
					}
					old := s.nodes.get(next)
					if old != nil && old.closed {
						continue
					}
					g := current.g + stepCost*horiz + math.Abs(float64(dy))
					if old != nil {
						if g < old.g {
							old.g, old.parent, old.f = g, current, g+heuristic(next, goal)
							s.open.fix(old.index)
						}
						continue
					}
					node := s.arena.alloc(next, g, g+heuristic(next, goal), current)
					s.nodes.put(next, node)
					s.open.push(node)
				}
			}
		}
	}
	return nil
}

func reconstructPath(current *pathNode) []cube.Pos {
	count := 0
	for n := current; n != nil && n.parent != nil; n = n.parent {
		count++
	}
	path := make([]cube.Pos, count)
	for i := count - 1; i >= 0; i-- {
		path[i] = current.pos
		current = current.parent
	}
	return path
}

func heuristic(a, b cube.Pos) float64 {
	dx, dy, dz := float64(a[0]-b[0]), float64(a[1]-b[1]), float64(a[2]-b[2])
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

func groundWalkableAndCost(tx *world.Tx, pos cube.Pos, p GroundProfile, rng cube.Range, radius, vertical int) (bool, float64) {
	if pos[1] < rng[0] || pos[1] > rng[1] {
		return false, 0
	}
	b := tx.Block(pos)
	fire := false
	if !isAir(b) {
		switch v := b.(type) {
		case block.Fire:
			fire = true
		case block.WoodDoor:
			if !v.Open && !p.AllowDoors {
				return false, 0
			}
		case block.CopperDoor:
			if !v.Open && !p.AllowDoors {
				return false, 0
			}
		}
		if len(b.Model().BBox(pos, tx)) != 0 {
			return false, 0
		}
	}
	if !solidBelow(tx, pos) {
		return false, 0
	}
	cost := 1.0
	if fire {
		cost += 16
	}
	if liquid, ok := tx.Liquid(pos); ok {
		switch liquid.(type) {
		case block.Lava:
			return false, 0
		default:
			if !p.AllowWater {
				return false, 0
			}
			cost += 8
		}
	}
	for dx := -radius; dx <= radius; dx++ {
		for dz := -radius; dz <= radius; dz++ {
			for dy := range vertical {
				if dx == 0 && dy == 0 && dz == 0 {
					continue
				}
				clearance := cube.Pos{pos[0] + dx, pos[1] + dy, pos[2] + dz}
				if clearance[1] < rng[0] || clearance[1] > rng[1] || !isPassable(tx, clearance) {
					return false, 0
				}
			}
		}
	}
	return true, cost
}

func solidBelow(tx *world.Tx, pos cube.Pos) bool {
	below := cube.Pos{pos[0], pos[1] - 1, pos[2]}
	b := tx.Block(below)
	if isAir(b) {
		return false
	}
	return len(b.Model().BBox(below, tx)) > 0
}

func isPassable(tx *world.Tx, pos cube.Pos) bool {
	b := tx.Block(pos)
	if isAir(b) {
		return true
	}
	return len(b.Model().BBox(pos, tx)) == 0
}
