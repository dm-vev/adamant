package entity

import (
	"math"
	"sync"

	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/ecs"
	"github.com/dm-vev/adamant/server/world"
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

// pathNodeHash mixes a packed node key for open-addressed table indexing.
func pathNodeHash(key int64) uint64 {
	h := uint64(key) * 0x9e3779b97f4a7c15
	return h ^ (h >> 32)
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

// pathNodeSlabSize is the number of pathNodeData slots per slab in pathNodeArena.
const pathNodeSlabSize = 64

// pathNodeArena allocates pathNodeData values from reusable slabs so node
// pointers remain stable for the duration of a search without per-node heap
// allocations.
type pathNodeArena struct {
	slabs   []*[pathNodeSlabSize]pathNodeData
	slabIdx int
	slotIdx int
}

func (a *pathNodeArena) reset() {
	a.slabIdx, a.slotIdx = 0, 0
}

func (a *pathNodeArena) alloc(x, y, z int) *pathNodeData {
	if a.slotIdx == pathNodeSlabSize {
		a.slabIdx++
		a.slotIdx = 0
	}
	if a.slabIdx >= len(a.slabs) {
		a.slabs = append(a.slabs, new([pathNodeSlabSize]pathNodeData))
	}
	n := &a.slabs[a.slabIdx][a.slotIdx]
	a.slotIdx++
	*n = pathNodeData{x: x, y: y, z: z, heapIdx: -1}
	return n
}

// pathNodeTable is a generation-stamped open-addressed table mapping packed
// node keys to arena-allocated pathNodeData pointers. Resetting between
// searches is O(1).
type pathNodeTable struct {
	entries []pathNodeTableEntry
	mask    uint64
	count   int
	gen     uint32
}

type pathNodeTableEntry struct {
	key  int64
	node *pathNodeData
	gen  uint32
}

func (t *pathNodeTable) reset() {
	if len(t.entries) == 0 {
		t.entries = make([]pathNodeTableEntry, 256)
		t.mask = 255
	}
	t.count = 0
	t.gen++
	if t.gen == 0 {
		clear(t.entries)
		t.gen = 1
	}
}

func (t *pathNodeTable) grow() {
	old := t.entries
	newCap := len(old) * 2
	t.entries = make([]pathNodeTableEntry, newCap)
	t.mask = uint64(newCap - 1)
	gen := t.gen
	for i := range old {
		if old[i].gen != gen {
			continue
		}
		idx := pathNodeHash(old[i].key) & t.mask
		for t.entries[idx].gen == gen {
			idx = (idx + 1) & t.mask
		}
		t.entries[idx] = old[i]
	}
}

func (t *pathNodeTable) getOrAlloc(x, y, z int, arena *pathNodeArena) *pathNodeData {
	if (t.count+1)*2 >= len(t.entries) {
		t.grow()
	}
	key := pathNodeKey(x, y, z)
	idx := pathNodeHash(key) & t.mask
	gen := t.gen
	for {
		e := &t.entries[idx]
		if e.gen != gen {
			n := arena.alloc(x, y, z)
			e.key, e.node, e.gen = key, n, gen
			t.count++
			return n
		}
		if e.key == key {
			return e.node
		}
		idx = (idx + 1) & t.mask
	}
}

// pathTypeTable is a generation-stamped open-addressed table caching PathType
// values by packed position key for one search.
type pathTypeTable struct {
	entries []pathTypeTableEntry
	mask    uint64
	count   int
	gen     uint32
}

type pathTypeTableEntry struct {
	key int64
	gen uint32
	typ PathType
}

func (t *pathTypeTable) reset() {
	if len(t.entries) == 0 {
		t.entries = make([]pathTypeTableEntry, 512)
		t.mask = 511
	}
	t.count = 0
	t.gen++
	if t.gen == 0 {
		clear(t.entries)
		t.gen = 1
	}
}

func (t *pathTypeTable) grow() {
	old := t.entries
	newCap := len(old) * 2
	t.entries = make([]pathTypeTableEntry, newCap)
	t.mask = uint64(newCap - 1)
	gen := t.gen
	for i := range old {
		if old[i].gen != gen {
			continue
		}
		idx := pathNodeHash(old[i].key) & t.mask
		for t.entries[idx].gen == gen {
			idx = (idx + 1) & t.mask
		}
		t.entries[idx] = old[i]
	}
}

func (t *pathTypeTable) get(key int64) (PathType, bool) {
	idx := pathNodeHash(key) & t.mask
	gen := t.gen
	for {
		e := &t.entries[idx]
		if e.gen != gen {
			return 0, false
		}
		if e.key == key {
			return e.typ, true
		}
		idx = (idx + 1) & t.mask
	}
}

func (t *pathTypeTable) put(key int64, typ PathType) {
	if (t.count+1)*2 >= len(t.entries) {
		t.grow()
	}
	idx := pathNodeHash(key) & t.mask
	gen := t.gen
	for {
		e := &t.entries[idx]
		if e.gen != gen {
			e.key, e.typ, e.gen = key, typ, gen
			t.count++
			return
		}
		if e.key == key {
			e.typ = typ
			return
		}
		idx = (idx + 1) & t.mask
	}
}

// pathFloorTable is a generation-stamped open-addressed table caching
// floorLevel results for one walk search.
type pathFloorTable struct {
	entries []pathFloorTableEntry
	mask    uint64
	count   int
	gen     uint32
}

type pathFloorTableEntry struct {
	key   int64
	floor float64
	gen   uint32
}

func (t *pathFloorTable) reset() {
	if len(t.entries) == 0 {
		t.entries = make([]pathFloorTableEntry, 256)
		t.mask = 255
	}
	t.count = 0
	t.gen++
	if t.gen == 0 {
		clear(t.entries)
		t.gen = 1
	}
}

func (t *pathFloorTable) grow() {
	old := t.entries
	newCap := len(old) * 2
	t.entries = make([]pathFloorTableEntry, newCap)
	t.mask = uint64(newCap - 1)
	gen := t.gen
	for i := range old {
		if old[i].gen != gen {
			continue
		}
		idx := pathNodeHash(old[i].key) & t.mask
		for t.entries[idx].gen == gen {
			idx = (idx + 1) & t.mask
		}
		t.entries[idx] = old[i]
	}
}

func (t *pathFloorTable) get(key int64) (float64, bool) {
	idx := pathNodeHash(key) & t.mask
	gen := t.gen
	for {
		e := &t.entries[idx]
		if e.gen != gen {
			return 0, false
		}
		if e.key == key {
			return e.floor, true
		}
		idx = (idx + 1) & t.mask
	}
}

func (t *pathFloorTable) put(key int64, floor float64) {
	if (t.count+1)*2 >= len(t.entries) {
		t.grow()
	}
	idx := pathNodeHash(key) & t.mask
	gen := t.gen
	for {
		e := &t.entries[idx]
		if e.gen != gen {
			e.key, e.floor, e.gen = key, floor, gen
			t.count++
			return
		}
		if e.key == key {
			e.floor = floor
			return
		}
		idx = (idx + 1) & t.mask
	}
}

// pathSearchScratch holds all reusable working memory for a single A* search,
// kept per world-owner / prefetch-worker goroutine via pathWorldLocal.
type pathSearchScratch struct {
	inUse    bool
	fromPool bool

	open       pathHeapQueue
	arena      pathNodeArena
	nodes      pathNodeTable
	types      pathTypeTable
	blockTypes pathTypeTable
	floors     pathFloorTable
	collisions map[cube.BBox]bool

	neighbours [32]*pathNodeData
	malus      [pathTypeCount]float64
	reader     pathStateReader
}

type livingEntityDist struct {
	h    *world.EntityHandle
	dist float64
}

// pathWorldLocal bundles per-goroutine pathfinding caches stored in Tx.Local
// and ReadView.Local so both the world owner and each parallel prefetch worker
// get lock-free, L1-warm reuse.
type pathWorldLocal struct {
	typeCache   pathTypeCache
	scratch     pathSearchScratch
	ground      groundPathScratch
	livingFound []livingEntityDist
}

type pathWorldLocalKey struct{}

func newPathWorldLocal() any { return &pathWorldLocal{} }

var pathSearchScratchPool = sync.Pool{
	New: func() any { return &pathSearchScratch{} },
}

func worldLocalFor(pw pathWorld) *pathWorldLocal {
	switch w := pw.(type) {
	case *world.Tx:
		return w.Local(pathWorldLocalKey{}, newPathWorldLocal).(*pathWorldLocal)
	case *world.ReadView:
		return w.Local(pathWorldLocalKey{}, newPathWorldLocal).(*pathWorldLocal)
	default:
		return nil
	}
}

func acquirePathScratch(pw pathWorld) (*pathSearchScratch, *pathTypeCache) {
	if local := worldLocalFor(pw); local != nil {
		if !local.scratch.inUse {
			local.scratch.inUse = true
			return &local.scratch, &local.typeCache
		}
		s := pathSearchScratchPool.Get().(*pathSearchScratch)
		s.inUse, s.fromPool = true, true
		return s, &local.typeCache
	}
	s := pathSearchScratchPool.Get().(*pathSearchScratch)
	s.inUse, s.fromPool = true, true
	return s, nil
}

func releasePathScratch(s *pathSearchScratch) {
	s.open.reset()
	s.arena.reset()
	clear(s.neighbours[:])
	if len(s.collisions) > 0 {
		clear(s.collisions)
	}
	s.reader.pw, s.reader.tx, s.reader.rv, s.reader.cache = nil, nil, nil, nil
	s.reader.tracking = false
	s.inUse = false
	if s.fromPool {
		s.fromPool = false
		pathSearchScratchPool.Put(s)
	}
}

// pathHeapQueue is the open set of a path finder, ported from Java's
// BinaryHeap.
type pathHeapQueue struct {
	heap []*pathNodeData
}

func (h *pathHeapQueue) reset() {
	if len(h.heap) > 0 {
		clear(h.heap)
		h.heap = h.heap[:0]
	}
}

func (h *pathHeapQueue) insert(n *pathNodeData) {
	h.heap = append(h.heap, n)
	n.heapIdx = len(h.heap) - 1
	h.upHeap(n.heapIdx)
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
	nf := n.f
	for idx > 0 {
		parentIdx := (idx - 1) >> 1
		parent := h.heap[parentIdx]
		if !(nf < parent.f) {
			break
		}
		h.heap[idx] = parent
		parent.heapIdx = idx
		idx = parentIdx
	}
	h.heap[idx] = n
	n.heapIdx = idx
}

var posInf32 = float32(math.Inf(1))

func (h *pathHeapQueue) downHeap(idx int) {
	n := h.heap[idx]
	nf := n.f
	size := len(h.heap)
	for {
		left := 1 + idx<<1
		right := left + 1
		if left >= size {
			break
		}
		leftNode := h.heap[left]
		var rightNode *pathNodeData
		rightCost := posInf32
		if right < size {
			rightNode = h.heap[right]
			rightCost = rightNode.f
		}
		if leftNode.f < rightCost {
			if !(leftNode.f < nf) {
				break
			}
			h.heap[idx] = leftNode
			leftNode.heapIdx = idx
			idx = left
		} else {
			if !(rightCost < nf) {
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

// nodeEvaluator finds the nodes a mob may move to, as Java's NodeEvaluator.
type nodeEvaluator interface {
	pathWorld(m *Mob) pathWorld
	prepare(m *Mob, scratch *pathSearchScratch, cache *pathTypeCache)
	done()
	start() *pathNodeData
	neighbours(out []*pathNodeData, pos *pathNodeData) int
}

// updateTargetHeuristics computes the minimum Euclidean distance from n to any
// target, updating each target's closest visited node along the way.
func updateTargetHeuristics(ts []pathTarget, n *pathNodeData) float32 {
	if len(ts) == 1 {
		t := &ts[0]
		xd, yd, zd := float32(t.pos[0]-n.x), float32(t.pos[1]-n.y), float32(t.pos[2]-n.z)
		h := float32(math.Sqrt(float64(xd*xd + yd*yd + zd*zd)))
		if h < t.bestHeuristic {
			t.bestHeuristic, t.bestNode = h, n
		}
		return h
	}
	best := float32(math.MaxFloat32)
	for i := range ts {
		t := &ts[i]
		xd, yd, zd := float32(t.pos[0]-n.x), float32(t.pos[1]-n.y), float32(t.pos[2]-n.z)
		h := float32(math.Sqrt(float64(xd*xd + yd*yd + zd*zd)))
		t.updateBest(h, n)
		best = min(best, h)
	}
	return best
}

// selectBestMobPath reconstructs and picks the winning path across targets.
func selectBestMobPath(ts []pathTarget, reached bool) *mobPath {
	var best *mobPath
	for i := range ts {
		t := &ts[i]
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

// findPath finds a path from the mob to one of the targets with the A*
// search of Java's PathFinder.findPath.
func findPath(m *Mob, eval nodeEvaluator, targets []cube.Pos, maxPathLength float32, reachRange int, maxVisitedNodes int) *mobPath {
	return findPathOpt(m, eval, targets, maxPathLength, reachRange, maxVisitedNodes, true)
}

func findPathUncached(m *Mob, eval nodeEvaluator, targets []cube.Pos, maxPathLength float32, reachRange int, maxVisitedNodes int) *mobPath {
	return findPathOpt(m, eval, targets, maxPathLength, reachRange, maxVisitedNodes, false)
}

func findPathOpt(m *Mob, eval nodeEvaluator, targets []cube.Pos, maxPathLength float32, reachRange int, maxVisitedNodes int, useCache bool) *mobPath {
	scratch, typeCache := acquirePathScratch(eval.pathWorld(m))
	defer releasePathScratch(scratch)
	walkEval, isWalk := eval.(*walkNodeEvaluator)
	cacheEligible := useCache && isWalk && len(targets) == 1 && len(m.AI.maluses) == 0 && walkEval.blocked == nil
	eval.prepare(m, scratch, typeCache)
	defer eval.done()

	from := eval.start()
	if from == nil {
		return nil
	}
	var cache *sharedPathCache
	var key sharedPathKey
	if cacheEligible {
		var cached *mobPath
		var hit bool
		cached, cache, key, hit = trySharedPathLookup(walkEval, from, targets[0], reachRange, maxPathLength, maxVisitedNodes)
		if hit {
			return cached
		}
	}
	var targetBuf [4]pathTarget
	var ts []pathTarget
	if len(targets) <= len(targetBuf) {
		ts = targetBuf[:len(targets)]
	} else {
		ts = make([]pathTarget, len(targets))
	}
	for i, pos := range targets {
		ts[i] = pathTarget{pos: pos, bestHeuristic: math.MaxFloat32}
	}

	from.g = 0
	from.h = updateTargetHeuristics(ts, from)
	from.f = from.h
	open := &scratch.open
	open.reset()
	open.insert(from)

	reached := false
	reachF := float32(reachRange)
	count := 0
	neighbours := scratch.neighbours[:]
	for len(open.heap) > 0 {
		if count++; count >= maxVisitedNodes {
			break
		}
		current := open.pop()
		current.closed = true
		if len(ts) == 1 {
			t := &ts[0]
			if current.distanceManhattan(t.pos[0], t.pos[1], t.pos[2]) <= reachF {
				t.reached, reached = true, true
				break
			}
		} else {
			for i := range ts {
				t := &ts[i]
				if current.distanceManhattan(t.pos[0], t.pos[1], t.pos[2]) <= reachF {
					t.reached, reached = true, true
				}
			}
			if reached {
				break
			}
		}
		if current.distanceTo(from) >= maxPathLength {
			continue
		}
		nCount := eval.neighbours(neighbours, current)
		for i := range nCount {
			n := neighbours[i]
			distance := current.distanceTo(n)
			n.walkedDistance = current.walkedDistance + distance
			tentative := current.g + distance + n.costMalus
			if n.walkedDistance < maxPathLength && (!n.inOpenSet() || tentative < n.g) {
				n.cameFrom = current
				n.g = tentative
				n.h = updateTargetHeuristics(ts, n) * 1.5
				if n.inOpenSet() {
					open.changeCost(n, n.g+n.h)
				} else {
					n.f = n.g + n.h
					open.insert(n)
				}
			}
		}
	}
	best := selectBestMobPath(ts, reached)
	if cache != nil && best != nil {
		storeSharedPath(cache, key, walkEval, scratch, best)
	}
	return best
}

const (
	sharedPathShards     = 64
	sharedPathWays       = 4
	sharedPathMaxTracked = 16
)

var sharedPathCacheKey = ecs.NewKey[*sharedPathCache]("entity:shared_path_cache")

type sharedPathKey struct {
	start      cube.Pos
	target     cube.Pos
	reach      int32
	maxLenBits uint32
	maxFall    int32
	visited    int32
	entityW    int32
	entityH    int32
	mobWBits   uint64
	mobHBits   uint64
	stepBits   uint64
	config     walkNodeConfig
}

func (k sharedPathKey) hash() uint64 {
	h := uint64(k.start[0])*0x9e3779b97f4a7c15 ^ uint64(k.start[1])*0xbf58476d1ce4e5b9 ^ uint64(k.start[2])*0x94d049bb133111eb
	h ^= uint64(k.target[0])*0x517cc1b727220a95 ^ uint64(k.target[1])*0x2545f4914f6cdd1d ^ uint64(k.target[2])*0x85ebca6b
	h ^= uint64(k.reach)*0xc2b2ae35 ^ uint64(k.maxLenBits)*0x27d4eb2f ^ uint64(k.maxFall)*0x165667b1
	h ^= uint64(k.visited)*0x9e3779b97f4a7c15 ^ uint64(k.entityW)<<16 ^ uint64(k.entityH)<<24
	h ^= k.mobWBits ^ (k.mobHBits * 0xbf58476d1ce4e5b9) ^ (k.stepBits * 0x94d049bb133111eb)
	cfg := k.config
	var cfgBits uint64
	if cfg.canPassDoors {
		cfgBits |= 1 << 0
	}
	if cfg.canOpenDoors {
		cfgBits |= 1 << 1
	}
	if cfg.canFloat {
		cfgBits |= 1 << 2
	}
	if cfg.canWalkOverFences {
		cfgBits |= 1 << 3
	}
	if cfg.amphibious {
		cfgBits |= 1 << 4
	}
	if cfg.prefersShallow {
		cfgBits |= 1 << 5
	}
	if cfg.frog {
		cfgBits |= 1 << 6
	}
	if cfg.fly {
		cfgBits |= 1 << 7
	}
	h ^= cfgBits * 0x9e3779b97f4a7c15
	h ^= h >> 30
	h *= 0xbf58476d1ce4e5b9
	h ^= h >> 27
	return h
}

type sharedPathEntry struct {
	key          sharedPathKey
	valid        bool
	colCount     uint8
	reached      bool
	distToTarget float32
	lastUse      uint64
	cols         [sharedPathMaxTracked]world.ChunkPos
	vers         [sharedPathMaxTracked]uint64
	nodes        []*pathNodeData
}

type sharedPathShard struct {
	mu    sync.Mutex
	clock uint64
	ways  [sharedPathWays]sharedPathEntry
}

type sharedPathCache struct {
	shards [sharedPathShards]sharedPathShard
}

func ensureSharedPathCache(tx *world.Tx) *sharedPathCache {
	if tx == nil {
		return nil
	}
	w := tx.World()
	if w == nil {
		return nil
	}
	if c, ok := ecs.Get(w.Components(), sharedPathCacheKey); ok {
		return c
	}
	c := &sharedPathCache{}
	ecs.Set(w.Components(), sharedPathCacheKey, c)
	return c
}

func sharedPathCacheFor(pw pathWorld) *sharedPathCache {
	switch w := pw.(type) {
	case *world.Tx:
		return ensureSharedPathCache(w)
	case *world.ReadView:
		if wr := w.World(); wr != nil {
			c, _ := ecs.Get(wr.Components(), sharedPathCacheKey)
			return c
		}
	}
	return nil
}

func makeSharedPathKey(from *pathNodeData, target cube.Pos, reach int, maxLen float32, visited int, w *walkNodeEvaluator) sharedPathKey {
	return sharedPathKey{
		start:      cube.Pos{from.x, from.y, from.z},
		target:     target,
		reach:      int32(reach),
		maxLenBits: math.Float32bits(maxLen),
		maxFall:    int32(w.maxFall),
		visited:    int32(visited),
		entityW:    int32(w.entityWidth),
		entityH:    int32(w.entityHeight),
		mobWBits:   math.Float64bits(w.mobWidth),
		mobHBits:   math.Float64bits(w.mobHeight),
		stepBits:   math.Float64bits(w.maxStepUp),
		config:     w.config(),
	}
}

func clonePathNodes(src []*pathNodeData) []*pathNodeData {
	if len(src) == 0 {
		return nil
	}
	backing := make([]pathNodeData, len(src))
	nodes := make([]*pathNodeData, len(src))
	for i, n := range src {
		backing[i] = *n
		nodes[i] = &backing[i]
	}
	return nodes
}

func (c *sharedPathCache) lookup(pw pathWorld, key sharedPathKey) (*mobPath, bool) {
	if c == nil {
		return nil, false
	}
	shard := &c.shards[key.hash()&(sharedPathShards-1)]
	shard.mu.Lock()
	defer shard.mu.Unlock()
	for i := range shard.ways {
		w := &shard.ways[i]
		if !w.valid || w.key != key {
			continue
		}
		stale := false
		for ci := range int(w.colCount) {
			ver, ok := columnBlockVersion(pw, w.cols[ci])
			if !ok || ver != w.vers[ci] {
				stale = true
				break
			}
		}
		if stale {
			w.valid = false
			return nil, false
		}
		shard.clock++
		w.lastUse = shard.clock
		return &mobPath{
			nodes:        clonePathNodes(w.nodes),
			target:       key.target,
			distToTarget: w.distToTarget,
			reached:      w.reached,
		}, true
	}
	return nil, false
}

func (c *sharedPathCache) store(key sharedPathKey, path *mobPath, cols []world.ChunkPos, vers []uint64) {
	if c == nil || path == nil || len(cols) == 0 || len(cols) > sharedPathMaxTracked || len(cols) != len(vers) {
		return
	}
	nodes := clonePathNodes(path.nodes)
	shard := &c.shards[key.hash()&(sharedPathShards-1)]
	shard.mu.Lock()
	defer shard.mu.Unlock()
	slot := 0
	bestUse := ^uint64(0)
	for i := range shard.ways {
		w := &shard.ways[i]
		if w.valid && w.key == key {
			slot = i
			break
		}
		if !w.valid {
			slot = i
			break
		}
		if w.lastUse < bestUse {
			bestUse = w.lastUse
			slot = i
		}
	}
	shard.clock++
	e := &shard.ways[slot]
	e.key = key
	e.valid = true
	e.colCount = uint8(len(cols))
	e.reached = path.reached
	e.distToTarget = path.distToTarget
	e.lastUse = shard.clock
	e.nodes = nodes
	copy(e.cols[:], cols)
	copy(e.vers[:], vers)
}

func trySharedPathLookup(w *walkNodeEvaluator, from *pathNodeData, target cube.Pos, reach int, maxLen float32, visited int) (*mobPath, *sharedPathCache, sharedPathKey, bool) {
	pw := w.pw
	if rv, isRV := pw.(*world.ReadView); isRV && rv.Missed() {
		return nil, nil, sharedPathKey{}, false
	}
	cache := sharedPathCacheFor(pw)
	if cache == nil {
		return nil, nil, sharedPathKey{}, false
	}
	key := makeSharedPathKey(from, target, reach, maxLen, visited, w)
	if p, ok := cache.lookup(pw, key); ok {
		return p, nil, sharedPathKey{}, true
	}
	return nil, cache, key, false
}

func storeSharedPath(cache *sharedPathCache, key sharedPathKey, w *walkNodeEvaluator, scratch *pathSearchScratch, path *mobPath) {
	r := &scratch.reader
	if r.trackOverflow || r.trackCount == 0 {
		return
	}
	if rv, isRV := w.pw.(*world.ReadView); isRV && rv.Missed() {
		return
	}
	cache.store(key, path, r.trackCols[:r.trackCount], r.trackVers[:r.trackCount])
}

func reconstructMobPath(closest *pathNodeData, target cube.Pos, reached bool) *mobPath {
	count := 0
	for n := closest; n != nil; n = n.cameFrom {
		count++
	}
	backing := make([]pathNodeData, count)
	nodes := make([]*pathNodeData, count)
	i := count - 1
	for n := closest; n != nil; n = n.cameFrom {
		c := *n
		c.cameFrom = nil
		c.heapIdx = -1
		backing[i] = c
		nodes[i] = &backing[i]
		i--
	}
	return newMobPath(nodes, target, reached)
}
