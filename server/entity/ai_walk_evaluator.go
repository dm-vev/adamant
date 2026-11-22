package entity

import (
	"math"
	"math/bits"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// walkNodeEvaluator finds the nodes a walking mob may step to, ported from
// Java's WalkNodeEvaluator and NodeEvaluator.
type walkNodeEvaluator struct {
	canPassDoors, canOpenDoors, canFloat, canWalkOverFences bool
	// amphibious selects the evaluation of AmphibiousNodeEvaluator, which
	// makes deep water cost more if prefersShallow.
	amphibious, prefersShallow bool
	// frog selects the evaluation of Frog.FrogNodeEvaluator: a frog starts
	// at the bottom of its box in water, and finds open what stands on a
	// block it likes to jump to.
	frog bool
	// fly gives blocks the path types of FlyNodeEvaluator.
	fly bool
	// blocked, if set, blocks the positions it reports, as evaluators
	// overriding getPathType to return BLOCKED do.
	blocked func(m *Mob, pos cube.Pos) bool

	// The maluses an amphibious evaluator saves and restores while it finds a path.
	oldWalkable, oldWaterBorder float64
	hadWalkable, hadWaterBorder bool

	m *Mob
	// pw is what the evaluator reads the world through, the transaction
	// of m unless a parallel phase set a read view before prepare.
	pw      pathWorld
	scratch *pathSearchScratch
	// maxFall is Mob.getMaxFallDistance for the path being found.
	maxFall                                int
	entityWidth, entityHeight, entityDepth int
	multiCell                              bool
	mobWidth, mobHeight, maxStepUp         float64
	jumpStep                               int
	mobPos                                 cube.Pos
	mobOnRailChecked, mobOnRail            bool
	seaLevel                               int
	reusable                               [4]*pathNodeData
}

// horizontalDirections are Java's Direction.Plane.HORIZONTAL in order:
// north, east, south and west.
var horizontalDirections = [4]cube.Pos{{0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}}

// direction2D returns Direction.get2DDataValue of a horizontal direction.
func direction2D(i int) int { return [4]int{2, 3, 0, 1}[i] }

func (w *walkNodeEvaluator) pathWorld(m *Mob) pathWorld {
	if w.pw != nil {
		return w.pw
	}
	return m.tx
}

func (w *walkNodeEvaluator) prepare(m *Mob, scratch *pathSearchScratch, cache *pathTypeCache) {
	w.m = m
	w.scratch = scratch
	if w.pw == nil {
		w.pw, w.maxFall = m.tx, m.maxFallDistance()
	}
	if w.amphibious {
		// AmphibiousNodeEvaluator.prepare: swimming costs nothing, walking
		// costs and the water's edge too.
		w.oldWalkable, w.hadWalkable = m.AI.maluses[PathWalkable]
		w.oldWaterBorder, w.hadWaterBorder = m.AI.maluses[PathWaterBorder]
		m.AI.SetPathfindingMalus(PathWater, 0)
		m.AI.SetPathfindingMalus(PathWalkable, 6)
		m.AI.SetPathfindingMalus(PathWaterBorder, 4)
	}
	scratch.malus = pathMaluses
	for t, malus := range m.AI.maluses {
		scratch.malus[t] = malus
	}
	scratch.nodes.reset()
	scratch.types.reset()
	scratch.floors.reset()
	scratch.reader.init(w.pw, cache)

	bb := m.BBox()
	w.mobWidth, w.mobHeight = bb.Width(), bb.Height()
	w.entityWidth = int(math.Floor(w.mobWidth + 1))
	w.entityHeight = int(math.Floor(w.mobHeight + 1))
	w.entityDepth = w.entityWidth
	w.multiCell = w.entityWidth > 1 || w.entityHeight > 1 || w.entityDepth > 1
	if w.multiCell {
		scratch.blockTypes.reset()
	}
	w.maxStepUp = math.Max(1.125, m.AI.StepHeight)
	w.jumpStep = int(math.Floor(math.Max(1, m.AI.StepHeight)))
	w.mobPos = m.BlockPos()
	w.mobOnRailChecked, w.mobOnRail = false, false
	if w.amphibious && w.prefersShallow {
		w.seaLevel = seaLevel(m.tx)
	}
}

func (w *walkNodeEvaluator) done() {
	if w.amphibious && w.m != nil {
		if w.hadWalkable {
			w.m.AI.maluses[PathWalkable] = w.oldWalkable
		} else {
			delete(w.m.AI.maluses, PathWalkable)
		}
		if w.hadWaterBorder {
			w.m.AI.maluses[PathWaterBorder] = w.oldWaterBorder
		} else {
			delete(w.m.AI.maluses, PathWaterBorder)
		}
	}
	w.m, w.pw, w.scratch = nil, nil, nil
}

func (w *walkNodeEvaluator) node(x, y, z int) *pathNodeData {
	return w.scratch.nodes.getOrAlloc(x, y, z, &w.scratch.arena)
}

// blockAt returns the block at a position, or air if its chunk isn't
// loaded, as a PathNavigationRegion holds no unloaded chunks.
func (w *walkNodeEvaluator) blockAt(pos cube.Pos) world.Block {
	return pathBlockAt(w.pw, pos)
}

// pathWorld is what finding a path reads of a world: a transaction, or the
// read view of a world in the parallel phase before entities tick.
type pathWorld interface {
	blockReader
	Range() cube.Range
	BlockLoaded(pos cube.Pos) (world.Block, bool)
	BlockStateID(pos cube.Pos) (uint32, error)
}

var (
	_ pathWorld = (*world.Tx)(nil)
	_ pathWorld = (*world.ReadView)(nil)
)

func pathBlockAt(tx pathWorld, pos cube.Pos) world.Block {
	if pos.OutOfBounds(tx.Range()) {
		return block.Air{}
	}
	b, ok := tx.BlockLoaded(pos)
	if !ok {
		return block.Air{}
	}
	return b
}

// start ports WalkNodeEvaluator.getStart, and that of
// AmphibiousNodeEvaluator for a mob in water.
func (w *walkNodeEvaluator) start() *pathNodeData {
	m := w.m
	if w.amphibious && m.InWater() {
		bb := m.BBox()
		y := bb.Min()[1] + .5
		if w.frog {
			y = bb.Min()[1]
		}
		return w.startNode(int(math.Floor(bb.Min()[0])), int(math.Floor(y)), int(math.Floor(bb.Min()[2])))
	}
	pos := m.data.Pos
	x, z := int(math.Floor(pos[0])), int(math.Floor(pos[2]))
	startY := int(math.Floor(pos[1]))
	switch {
	case w.canFloat && m.InWater():
		for pathWater(w.pw, cube.Pos{x, startY, z}) {
			startY++
		}
		startY--
	case m.OnGround():
		startY = int(math.Floor(pos[1] + .5))
	default:
		y := int(math.Floor(pos[1] + 1))
		minY := w.scratch.reader.minY
		for y > minY {
			startY = y
			y--
			below := cube.Pos{x, y, z}
			if b := w.blockAt(below); !isAir(b) && !pathfindableLand(w.pw, below, b) {
				break
			}
		}
	}
	if !w.canStartAt(x, startY, z) {
		bb := m.BBox()
		for _, corner := range [4][2]float64{{bb.Min()[0], bb.Min()[2]}, {bb.Min()[0], bb.Max()[2]}, {bb.Max()[0], bb.Min()[2]}, {bb.Max()[0], bb.Max()[2]}} {
			cx, cz := int(math.Floor(corner[0])), int(math.Floor(corner[1]))
			if w.canStartAt(cx, startY, cz) {
				return w.startNode(cx, startY, cz)
			}
		}
	}
	return w.startNode(x, startY, z)
}

func (w *walkNodeEvaluator) startNode(x, y, z int) *pathNodeData {
	n := w.node(x, y, z)
	n.typ = w.cachedPathType(x, y, z)
	n.costMalus = float32(w.scratch.malus[n.typ])
	return n
}

func (w *walkNodeEvaluator) canStartAt(x, y, z int) bool {
	t := w.cachedPathType(x, y, z)
	return t != PathOpen && w.scratch.malus[t] >= 0
}

// neighbours ports WalkNodeEvaluator.getNeighbors.
func (w *walkNodeEvaluator) neighbours(out []*pathNodeData, pos *pathNodeData) int {
	p, jumpSize := 0, 0
	above := w.cachedPathType(pos.x, pos.y+1, pos.z)
	current := w.cachedPathType(pos.x, pos.y, pos.z)
	if w.scratch.malus[above] >= 0 && current != PathStickyHoney {
		jumpSize = w.jumpStep
	}
	height := w.floorLevel(cube.Pos{pos.x, pos.y, pos.z})
	for i, d := range horizontalDirections {
		n := w.findAcceptedNode(pos.x+d[0], pos.y, pos.z+d[2], jumpSize, height, i, current)
		w.reusable[direction2D(i)] = n
		if n != nil && !n.closed && (n.costMalus >= 0 || pos.costMalus < 0) {
			out[p] = n
			p++
		}
	}
	for i, d := range horizontalDirections {
		second := (i + 1) & 3
		if !w.diagonalValid(pos, w.reusable[direction2D(i)], w.reusable[direction2D(second)]) {
			continue
		}
		s := horizontalDirections[second]
		n := w.findAcceptedNode(pos.x+d[0]+s[0], pos.y, pos.z+d[2]+s[2], jumpSize, height, i, current)
		if n != nil && !n.closed && n.typ != PathWalkableDoor && n.costMalus >= 0 {
			out[p] = n
			p++
		}
	}
	if w.amphibious {
		// AmphibiousNodeEvaluator.getNeighbors: a swimmer also moves up and
		// down through water.
		up := w.findAcceptedNode(pos.x, pos.y+1, pos.z, max(0, jumpSize-1), height, directionUp, current)
		down := w.findAcceptedNode(pos.x, pos.y-1, pos.z, jumpSize, height, directionDown, current)
		if up != nil && !up.closed && (up.costMalus >= 0 || pos.costMalus < 0) && up.typ == PathWater {
			out[p] = up
			p++
		}
		if down != nil && !down.closed && (down.costMalus >= 0 || pos.costMalus < 0) && down.typ == PathWater && current != PathTrapdoor {
			out[p] = down
			p++
		}
		if w.prefersShallow {
			shallowLimit := w.seaLevel - 10
			for _, n := range out[:p] {
				if n.typ == PathWater && n.y < shallowLimit {
					n.costMalus++
				}
			}
		}
	}
	return p
}

// directionUp and directionDown follow the horizontal directions for the
// vertical neighbours of amphibious paths.
const (
	directionUp = iota + 4
	directionDown
)

// directionStep returns the horizontal step of a direction, none for up and
// down.
func directionStep(dir int) cube.Pos {
	if dir < len(horizontalDirections) {
		return horizontalDirections[dir]
	}
	return cube.Pos{}
}

// diagonalValid ports WalkNodeEvaluator.isDiagonalValid(Node, Node, Node).
func (w *walkNodeEvaluator) diagonalValid(pos, ew, ns *pathNodeData) bool {
	if ns == nil || ew == nil || ns.y > pos.y || ew.y > pos.y {
		return false
	}
	if ew.typ == PathWalkableDoor || ns.typ == PathWalkableDoor {
		return false
	}
	if w.mobWidth > 1 && (ew.costMalus > 0 || ns.costMalus > 0) {
		return false
	}
	betweenPosts := ns.typ == PathFence && ew.typ == PathFence && w.mobWidth < .5
	return (ns.y < pos.y || ns.costMalus >= 0 || betweenPosts) && (ew.y < pos.y || ew.costMalus >= 0 || betweenPosts)
}

func partialCollision(t PathType) bool {
	return t == PathFence || t == PathDoorWoodClosed || t == PathDoorIronClosed
}

// canReachWithoutCollision ports the method of WalkNodeEvaluator.
func (w *walkNodeEvaluator) canReachWithoutCollision(to *pathNodeData) bool {
	bb := w.m.BBox()
	size := (bb.Width() + bb.Height() + bb.Length()) / 3
	delta := mgl64.Vec3{
		float64(to.x) - w.m.data.Pos[0] + bb.Width()/2,
		float64(to.y) - w.m.data.Pos[1] + bb.Height()/2,
		float64(to.z) - w.m.data.Pos[2] + bb.Length()/2,
	}
	steps := int(math.Ceil(delta.Len() / size))
	delta = delta.Mul(1 / float64(steps))
	for range steps {
		bb = bb.Translate(delta)
		if w.hasCollisions(bb) {
			return false
		}
	}
	return true
}

// floorLevel ports WalkNodeEvaluator.getFloorLevel, caching per-position
// results for the duration of one search.
func (w *walkNodeEvaluator) floorLevel(pos cube.Pos) float64 {
	key := pathNodeKey(pos[0], pos[1], pos[2])
	if fl, ok := w.scratch.floors.get(key); ok {
		return fl
	}
	var fl float64
	if (w.canFloat || w.amphibious) && pathWater(w.pw, pos) {
		fl = float64(pos[1]) + .5
	} else {
		fl = pathFloorLevel(w.pw, pos)
	}
	w.scratch.floors.put(key, fl)
	return fl
}

// pathFloorLevel returns the height of the floor of a position: the top of
// the collision shape of the block below.
func pathFloorLevel(tx pathWorld, pos cube.Pos) float64 {
	below := pos.Side(cube.FaceDown)
	b := pathBlockAt(tx, below)
	if isAir(b) {
		return float64(below.Y())
	}
	top, ok := collisionTop(tx, below, b)
	if !ok {
		return float64(below.Y())
	}
	return float64(below.Y()) + top
}

// collisionTop returns the top of the collision shape of a block.
func collisionTop(tx pathWorld, pos cube.Pos, b world.Block) (float64, bool) {
	boxes := b.Model().BBox(pos, tx)
	if len(boxes) == 0 {
		return 0, false
	}
	top := 0.0
	for _, bb := range boxes {
		top = math.Max(top, bb.Max()[1])
	}
	return top, true
}

// findAcceptedNode ports WalkNodeEvaluator.findAcceptedNode.
func (w *walkNodeEvaluator) findAcceptedNode(x, y, z, jumpSize int, nodeHeight float64, dir int, current PathType) *pathNodeData {
	var best *pathNodeData
	if w.floorLevel(cube.Pos{x, y, z})-nodeHeight > w.maxStepUp {
		return nil
	}
	t := w.cachedPathType(x, y, z)
	cost := w.scratch.malus[t]
	if cost >= 0 {
		best = w.nodeWithCost(x, y, z, t, cost)
	}
	if partialCollision(current) && best != nil && best.costMalus >= 0 && !w.canReachWithoutCollision(best) {
		best = nil
	}
	if t == PathWalkable || w.amphibious && t == PathWater {
		return best
	}
	switch {
	case (best == nil || best.costMalus < 0) && jumpSize > 0 && (t != PathFence || w.canWalkOverFences) && t != PathUnpassableRail && t != PathTrapdoor && t != PathPowderSnow:
		best = w.tryJumpOn(x, y, z, jumpSize, nodeHeight, dir, current)
	case !w.amphibious && t == PathWater && !w.canFloat:
		best = w.firstNonWaterBelow(x, y, z, best)
	case t == PathOpen:
		best = w.firstGroundNodeBelow(x, y, z)
	case partialCollision(t) && best == nil:
		best = w.closedNode(x, y, z, t)
	}
	return best
}

func (w *walkNodeEvaluator) nodeWithCost(x, y, z int, t PathType, cost float64) *pathNodeData {
	n := w.node(x, y, z)
	n.typ = t
	n.costMalus = max(n.costMalus, float32(cost))
	return n
}

func (w *walkNodeEvaluator) blockedNode(x, y, z int) *pathNodeData {
	n := w.node(x, y, z)
	n.typ, n.costMalus = PathBlocked, -1
	return n
}

func (w *walkNodeEvaluator) closedNode(x, y, z int, t PathType) *pathNodeData {
	n := w.node(x, y, z)
	n.closed, n.typ, n.costMalus = true, t, float32(t.Malus())
	return n
}

// tryJumpOn ports WalkNodeEvaluator.tryJumpOn.
func (w *walkNodeEvaluator) tryJumpOn(x, y, z, jumpSize int, nodeHeight float64, dir int, current PathType) *pathNodeData {
	above := w.findAcceptedNode(x, y+1, z, jumpSize-1, nodeHeight, dir, current)
	if above == nil {
		return nil
	}
	if w.mobWidth >= 1 || above.typ != PathOpen && above.typ != PathWalkable {
		return above
	}
	d := directionStep(dir)
	cx, cz := float64(x-d[0])+.5, float64(z-d[2])+.5
	half := w.mobWidth / 2
	floorFrom := w.floorLevel(cube.PosFromVec3(mgl64.Vec3{cx, float64(y + 1), cz}))
	floorTo := w.floorLevel(cube.Pos{above.x, above.y, above.z})
	box := cube.Box(cx-half, floorFrom+.001, cz-half, cx+half, w.mobHeight+floorTo-.002, cz+half)
	if w.hasCollisions(box) {
		return nil
	}
	return above
}

func (w *walkNodeEvaluator) firstNonWaterBelow(x, y, z int, best *pathNodeData) *pathNodeData {
	minY := w.scratch.reader.minY
	for y--; y > minY; y-- {
		t := w.cachedPathType(x, y, z)
		if t != PathWater {
			return best
		}
		best = w.nodeWithCost(x, y, z, t, w.scratch.malus[t])
	}
	return best
}

func (w *walkNodeEvaluator) firstGroundNodeBelow(x, y, z int) *pathNodeData {
	maxFall := w.maxFall
	minY := w.scratch.reader.minY
	for current := y - 1; current >= minY; current-- {
		if y-current > maxFall {
			return w.blockedNode(x, current, z)
		}
		t := w.cachedPathType(x, current, z)
		cost := w.scratch.malus[t]
		if t != PathOpen {
			if cost >= 0 {
				return w.nodeWithCost(x, current, z, t, cost)
			}
			return w.blockedNode(x, current, z)
		}
	}
	return w.blockedNode(x, y, z)
}

// hasCollisions reports if a block collides with a box.
func (w *walkNodeEvaluator) hasCollisions(box cube.BBox) bool {
	if w.scratch.collisions != nil {
		if c, ok := w.scratch.collisions[box]; ok {
			return c
		}
	}
	c := !boxFree(w.pw, box)
	if w.scratch.collisions == nil {
		w.scratch.collisions = make(map[cube.BBox]bool, 16)
	}
	w.scratch.collisions[box] = c
	return c
}

// boxFree reports if no block collides with a box.
func boxFree(tx pathWorld, box cube.BBox) bool {
	minX, minY, minZ := int(math.Floor(box.Min()[0])), int(math.Floor(box.Min()[1]))-1, int(math.Floor(box.Min()[2]))
	maxX, maxY, maxZ := int(math.Floor(box.Max()[0])), int(math.Floor(box.Max()[1])), int(math.Floor(box.Max()[2]))
	for x := minX; x <= maxX; x++ {
		for y := minY; y <= maxY; y++ {
			for z := minZ; z <= maxZ; z++ {
				pos := cube.Pos{x, y, z}
				b := pathBlockAt(tx, pos)
				if isAir(b) {
					continue
				}
				for _, bb := range b.Model().BBox(pos, tx) {
					if bb.Translate(pos.Vec3()).IntersectsWith(box) {
						return false
					}
				}
			}
		}
	}
	return true
}

func (w *walkNodeEvaluator) cachedPathType(x, y, z int) PathType {
	key := pathNodeKey(x, y, z)
	if t, ok := w.scratch.types.get(key); ok {
		return t
	}
	t := w.pathTypeOfMob(x, y, z)
	w.scratch.types.put(key, t)
	return t
}

func (w *walkNodeEvaluator) cachedBlockType(x, y, z int) PathType {
	if !w.multiCell {
		return w.blockType(cube.Pos{x, y, z})
	}
	key := pathNodeKey(x, y, z)
	if t, ok := w.scratch.blockTypes.get(key); ok {
		return t
	}
	t := w.blockType(cube.Pos{x, y, z})
	w.scratch.blockTypes.put(key, t)
	return t
}

func (w *walkNodeEvaluator) isMobOnRail() bool {
	if !w.mobOnRailChecked {
		w.mobOnRailChecked = true
		w.mobOnRail = w.scratch.reader.pathTypeStatic(w.mobPos) == PathRail ||
			w.scratch.reader.pathTypeStatic(w.mobPos.Side(cube.FaceDown)) == PathRail
	}
	return w.mobOnRail
}

// pathTypeOfMob ports WalkNodeEvaluator.getPathTypeOfMob: the type of the
// blocks the mob would stand in.
func (w *walkNodeEvaluator) pathTypeOfMob(x, y, z int) PathType {
	var mask uint32
	count := 0
	var only, originRaw PathType
	for dx := range w.entityWidth {
		for dy := range w.entityHeight {
			for dz := range w.entityDepth {
				t := w.cachedBlockType(x+dx, y+dy, z+dz)
				if dx == 0 && dy == 0 && dz == 0 {
					originRaw = t
				}
				if t == PathDoorWoodClosed && w.canOpenDoors && w.canPassDoors {
					t = PathWalkableDoor
				}
				if t == PathDoorOpen && !w.canPassDoors {
					t = PathBlocked
				}
				if t == PathRail && !w.isMobOnRail() {
					t = PathUnpassableRail
				}
				bit := uint32(1) << t
				if mask&bit == 0 {
					mask |= bit
					count++
					only = t
				}
			}
		}
	}
	if count == 1 {
		return only
	}
	if mask&(1<<PathFence) != 0 {
		return PathFence
	}
	if mask&(1<<PathUnpassableRail) != 0 {
		return PathUnpassableRail
	}
	malusTable := &w.scratch.malus
	highest, highestMalus := PathBlocked, malusTable[PathBlocked]
	for rem := mask; rem != 0; rem &= rem - 1 {
		t := PathType(bits.TrailingZeros32(rem))
		malus := malusTable[t]
		if malus < 0 {
			return t
		}
		if malus >= highestMalus {
			highest, highestMalus = t, malus
		}
	}
	if w.entityWidth > 1 {
		cheaper := malusTable[originRaw] < highestMalus
		if cheaper && malusTable[PathBigMobsCloseToDanger] < highestMalus {
			return PathBigMobsCloseToDanger
		}
		return highest
	}
	if originRaw == PathOpen && highest != PathOpen && highestMalus == 0 {
		return PathOpen
	}
	return highest
}

// frogPreferJumpTo checks whether the block below pos is in the
// frog_prefer_jump_to tag using the block state ID bitset when available.
func (w *walkNodeEvaluator) frogPreferJumpTo(below cube.Pos) bool {
	if below[1] < w.scratch.reader.minY || below[1] > w.scratch.reader.maxY {
		return false
	}
	if id, err := w.pw.BlockStateID(below); err == nil && id != 0 {
		in, _ := gamedata.BlockTagContainsID("frog_prefer_jump_to", id)
		return in
	}
	return blockInTag(w.blockAt(below), "frog_prefer_jump_to")
}

// blockType returns the path type of a block, as WalkNodeEvaluator.getPathType
// and those of AmphibiousNodeEvaluator, to which water next to a solid block
// is its border, and FlyNodeEvaluator.
func (w *walkNodeEvaluator) blockType(pos cube.Pos) PathType {
	if w.blocked != nil && w.blocked(w.m, pos) {
		return PathBlocked
	}
	if w.fly {
		return w.scratch.reader.flyPathType(w.mobPos, pos)
	}
	if w.frog && w.frogPreferJumpTo(pos.Side(cube.FaceDown)) {
		return PathOpen
	}
	r := &w.scratch.reader
	if !w.amphibious || r.stateAt(pos[0], pos[1], pos[2]) != PathWater {
		return r.pathTypeStatic(pos)
	}
	for _, f := range cube.Faces() {
		s := pos.Side(f)
		if r.stateAt(s[0], s[1], s[2]) == PathBlocked {
			return PathWaterBorder
		}
	}
	return PathWater
}

// pathType returns the path type of a single block for the mob, as
// NodeEvaluator.getPathType.
func (w *walkNodeEvaluator) pathType(m *Mob, pos cube.Pos) PathType {
	if w.fly {
		return flyPathType(m, pos)
	}
	return pathTypeStatic(m.tx, pos)
}

// pathChunkVer caches one column's block version for the duration of a single
// immutable pathfinding or classification pass.
type pathChunkVer struct {
	pos     world.ChunkPos
	version uint64
	loaded  bool
}

// pathStateReader amortizes world.Local, world.Range, and ColumnBlockVersion
// lookups across an immutable read pass (one findPath, pathTypeStatic, or
// RandomPos sampling call).
type pathStateReader struct {
	pw        pathWorld
	tx        *world.Tx
	rv        *world.ReadView
	cache     *pathTypeCache
	minY      int
	maxY      int
	keySafeY  bool
	lastCX    int32
	lastCZ    int32
	lastVer   uint64
	lastLoad  bool
	hasLast   bool
	validMask uint16
	chunks    [16]pathChunkVer
}

func (r *pathStateReader) init(pw pathWorld, cache *pathTypeCache) {
	r.pw = pw
	r.cache = cache
	r.hasLast = false
	r.validMask = 0
	rng := pw.Range()
	r.minY, r.maxY = rng.Min(), rng.Max()
	r.keySafeY = r.minY >= -2048 && r.maxY <= 2047
	r.tx, r.rv = nil, nil
	switch w := pw.(type) {
	case *world.Tx:
		r.tx = w
	case *world.ReadView:
		r.rv = w
	}
}

func (r *pathStateReader) initFromWorld(pw pathWorld) {
	var cache *pathTypeCache
	if local := worldLocalFor(pw); local != nil {
		cache = &local.typeCache
	}
	r.init(pw, cache)
}

func (r *pathStateReader) chunkVersionSlow(cx, cz int32) (uint64, bool) {
	chunk := world.ChunkPos{cx, cz}
	slot := uint(((cx & 3) << 2) | (cz & 3))
	if r.validMask&(1<<slot) != 0 && r.chunks[slot].pos == chunk {
		e := r.chunks[slot]
		r.lastCX, r.lastCZ, r.lastVer, r.lastLoad, r.hasLast = cx, cz, e.version, e.loaded, true
		return e.version, e.loaded
	}
	var ver uint64
	var loaded bool
	if r.tx != nil {
		ver, loaded = r.tx.ColumnBlockVersion(chunk)
	} else if r.rv != nil {
		ver, loaded = r.rv.ColumnBlockVersion(chunk)
	}
	r.chunks[slot] = pathChunkVer{pos: chunk, version: ver, loaded: loaded}
	r.validMask |= 1 << slot
	r.lastCX, r.lastCZ, r.lastVer, r.lastLoad, r.hasLast = cx, cz, ver, loaded, true
	return ver, loaded
}

func (r *pathStateReader) chunkVersion(cx, cz int32) (uint64, bool) {
	if r.hasLast && r.lastCX == cx && r.lastCZ == cz {
		return r.lastVer, r.lastLoad
	}
	return r.chunkVersionSlow(cx, cz)
}

func (r *pathStateReader) stateAtMiss(x, y, z int, key int64, ver uint64, idx int) PathType {
	t, ready := computePathTypeFromState(r.pw, cube.Pos{x, y, z})
	if ready {
		// A column still generating changes without a new version.
		r.cache.entries[idx] = pathTypeCacheEntry{key: key, verType: (ver << 8) | uint64(t)}
	}
	return t
}

func (r *pathStateReader) stateAt(x, y, z int) PathType {
	if y < r.minY || y > r.maxY {
		return PathOpen
	}
	if r.cache == nil {
		t, _ := computePathTypeFromState(r.pw, cube.Pos{x, y, z})
		return t
	}
	cx, cz := int32(x>>4), int32(z>>4)
	var ver uint64
	var loaded bool
	if r.hasLast && r.lastCX == cx && r.lastCZ == cz {
		ver, loaded = r.lastVer, r.lastLoad
	} else {
		ver, loaded = r.chunkVersionSlow(cx, cz)
	}
	if !loaded {
		return PathOpen
	}
	if !r.keySafeY || (uint64(x+(1<<25))|uint64(z+(1<<25))) >= (1<<26) || ver == 0 || ver >= (1<<56) {
		t, _ := computePathTypeFromState(r.pw, cube.Pos{x, y, z})
		return t
	}
	key := pathNodeKey(x, y, z)
	idx := pathTypeCacheSlot(key)
	e := r.cache.entries[idx]
	if e.key == key && (e.verType>>8) == ver {
		return PathType(e.verType)
	}
	return r.stateAtMiss(x, y, z, key, ver, idx)
}

func (r *pathStateReader) pathTypeStatic(pos cube.Pos) PathType {
	t := r.stateAt(pos[0], pos[1], pos[2])
	if t != PathOpen || pos[1] < r.minY+1 {
		return t
	}
	switch below := r.stateAt(pos[0], pos[1]-1, pos[2]); below {
	case PathOpen, PathWater, PathLava, PathWalkable:
		return PathOpen
	case PathFire, PathDamaging, PathStickyHoney, PathDamageCautious:
		return below
	case PathPowderSnow:
		return PathOnTopOfPowderSnow
	case PathTrapdoor:
		return PathOnTopOfTrapdoor
	}
	return r.checkNeighbourBlocks(pos, PathWalkable)
}

func (r *pathStateReader) checkNeighbourBlocks(pos cube.Pos, t PathType) PathType {
	x, y, z := pos[0], pos[1], pos[2]
	if r.cache != nil && r.keySafeY && y-1 >= r.minY && y+1 <= r.maxY &&
		uint64(x+(1<<25)-1) < (1<<26)-2 && uint64(z+(1<<25)-1) < (1<<26)-2 {
		if (x&15) >= 1 && (x&15) <= 14 && (z&15) >= 1 && (z&15) <= 14 {
			ver, loaded := r.chunkVersion(int32(x>>4), int32(z>>4))
			if !loaded {
				return t
			}
			if ver != 0 && ver < (1<<56) {
				return r.checkNeighbourBlocksInterior(x, y, z, ver, t)
			}
		} else {
			return r.checkNeighbourBlocksBoundary(x, y, z, t)
		}
	}
	return r.checkNeighbourBlocksFallback(x, y, z, t)
}

func (r *pathStateReader) checkNeighbourBlocksInterior(x, y, z int, ver uint64, t PathType) PathType {
	entries := &r.cache.entries
	for dx := -1; dx <= 1; dx++ {
		nx := x + dx
		xKey := int64(nx&0x3ffffff) << 38
		for dy := -1; dy <= 1; dy++ {
			ny := y + dy
			xyKey := xKey | int64(ny&0xfff)
			for dz := -1; dz <= 1; dz++ {
				if dx == 0 && dz == 0 {
					continue
				}
				nz := z + dz
				key := xyKey | int64(nz&0x3ffffff)<<12
				idx := pathTypeCacheSlot(key)
				e := entries[idx]
				var nt PathType
				if e.key == key && (e.verType>>8) == ver {
					nt = PathType(e.verType)
				} else {
					nt = r.stateAtMiss(nx, ny, nz, key, ver, idx)
				}
				switch nt {
				case PathDamaging:
					return PathDamagingInNeighbour
				case PathFire, PathLava:
					return PathFireInNeighbour
				case PathWater:
					return PathWaterBorder
				case PathDamageCautious:
					return PathDamageCautious
				}
			}
		}
	}
	return t
}

func (r *pathStateReader) checkNeighbourBlocksBoundary(x, y, z int, t PathType) PathType {
	entries := &r.cache.entries
	czs := [3]int32{int32((z - 1) >> 4), int32(z >> 4), int32((z + 1) >> 4)}
	for dx := -1; dx <= 1; dx++ {
		nx := x + dx
		cx := int32(nx >> 4)
		var vers [3]uint64
		var loads [3]bool
		vers[0], loads[0] = r.chunkVersion(cx, czs[0])
		if czs[1] == czs[0] {
			vers[1], loads[1] = vers[0], loads[0]
		} else {
			vers[1], loads[1] = r.chunkVersion(cx, czs[1])
		}
		if czs[2] == czs[1] {
			vers[2], loads[2] = vers[1], loads[1]
		} else {
			vers[2], loads[2] = r.chunkVersion(cx, czs[2])
		}
		xKey := int64(nx&0x3ffffff) << 38
		for dy := -1; dy <= 1; dy++ {
			ny := y + dy
			xyKey := xKey | int64(ny&0xfff)
			for dz := -1; dz <= 1; dz++ {
				if dx == 0 && dz == 0 || !loads[dz+1] {
					continue
				}
				nz := z + dz
				ver := vers[dz+1]
				var nt PathType
				if ver == 0 || ver >= (1<<56) {
					nt, _ = computePathTypeFromState(r.pw, cube.Pos{nx, ny, nz})
				} else {
					key := xyKey | int64(nz&0x3ffffff)<<12
					idx := pathTypeCacheSlot(key)
					e := entries[idx]
					if e.key == key && (e.verType>>8) == ver {
						nt = PathType(e.verType)
					} else {
						nt = r.stateAtMiss(nx, ny, nz, key, ver, idx)
					}
				}
				switch nt {
				case PathDamaging:
					return PathDamagingInNeighbour
				case PathFire, PathLava:
					return PathFireInNeighbour
				case PathWater:
					return PathWaterBorder
				case PathDamageCautious:
					return PathDamageCautious
				}
			}
		}
	}
	return t
}

func (r *pathStateReader) checkNeighbourBlocksFallback(x, y, z int, t PathType) PathType {
	for dx := -1; dx <= 1; dx++ {
		for dy := -1; dy <= 1; dy++ {
			for dz := -1; dz <= 1; dz++ {
				if dx == 0 && dz == 0 {
					continue
				}
				switch r.stateAt(x+dx, y+dy, z+dz) {
				case PathDamaging:
					return PathDamagingInNeighbour
				case PathFire, PathLava:
					return PathFireInNeighbour
				case PathWater:
					return PathWaterBorder
				case PathDamageCautious:
					return PathDamageCautious
				}
			}
		}
	}
	return t
}

// pathTypeStatic ports WalkNodeEvaluator.getPathTypeStatic: the type of a
// block, where an open block above a floor is walkable unless the floor or
// a neighbour is dangerous.
func pathTypeStatic(tx pathWorld, pos cube.Pos) PathType {
	var r pathStateReader
	r.initFromWorld(tx)
	return r.pathTypeStatic(pos)
}

// checkNeighbourBlocks ports WalkNodeEvaluator.checkNeighbourBlocks.
func checkNeighbourBlocks(tx pathWorld, pos cube.Pos, t PathType) PathType {
	var r pathStateReader
	r.initFromWorld(tx)
	return r.checkNeighbourBlocks(pos, t)
}

// pathTypesByState caches the path types of blocks by Java block state ID,
// as the PathTypeCache of a ServerLevel caches them by position.
var pathTypesByState sync.Map

// pathTypeFromState ports WalkNodeEvaluator.getPathTypeFromState, reading
// the type through the world's PathTypeCache when read through a
// transaction.
func pathTypeFromState(pw pathWorld, pos cube.Pos) PathType {
	var r pathStateReader
	r.initFromWorld(pw)
	return r.stateAt(pos[0], pos[1], pos[2])
}

const (
	pathTypeCacheSize = 8192
	pathTypeCacheMask = pathTypeCacheSize - 1
)

// pathTypeCache ports PathTypeCache, which a ServerLevel keeps to look the
// path types of blocks up by position. Each 16-byte entry packs the column
// block version (high 56 bits) and cached PathType (low 8 bits) alongside the
// packed BlockPos key so invalidation on block changes is automatic.
type pathTypeCache struct {
	entries [pathTypeCacheSize]pathTypeCacheEntry
}

type pathTypeCacheEntry struct {
	key     int64
	verType uint64
}

// pathTypeCacheSlot mixes a packed position key into a pathTypeCache slot.
func pathTypeCacheSlot(key int64) int {
	h := uint64(key) * 0x9e3779b97f4a7c15
	h ^= h >> 32
	h ^= h >> 16
	return int(h & pathTypeCacheMask)
}

// computePathTypeFromState computes what pathTypeFromState returns for a
// block and reports whether its column was ready to be read.
func computePathTypeFromState(tx pathWorld, pos cube.Pos) (PathType, bool) {
	b, ok := tx.BlockLoaded(pos)
	if !ok {
		return PathOpen, false
	}
	if _, air := b.(block.Air); air {
		if _, wet := tx.Liquid(pos); !wet {
			return PathOpen, true
		}
	}
	id, err := tx.BlockStateID(pos)
	if err == nil && id != 0 {
		if t, ok := pathTypesByState.Load(id); ok {
			return t.(PathType), true
		}
	}
	t := computePathType(tx, pos, b, id)
	if err == nil && id != 0 {
		pathTypesByState.Store(id, t)
	}
	return t, true
}

func computePathType(tx pathWorld, pos cube.Pos, b world.Block, id uint32) PathType {
	name, props := "", map[string]any(nil)
	if id != 0 {
		name, props, _ = gamedata.BlockStateFromID(id)
	} else if state, err := world.CanonicalStateOf(b); err == nil {
		name, props = state.Name, state.Properties
	}
	name = strings.TrimPrefix(name, "minecraft:")
	tagged := func(tag string) bool {
		in, _ := gamedata.BlockTagContains(tag, "minecraft:"+name)
		return in
	}
	liquid, wet := tx.Liquid(pos)
	lava := wet && liquid.LiquidType() == "lava"
	water := wet && liquid.LiquidType() == "water"
	switch {
	case name == "air" || name == "cave_air" || name == "void_air" || name == "":
		if lava {
			return PathLava
		}
		if water {
			return PathWater
		}
		if name == "" && len(b.Model().BBox(pos, tx)) != 0 {
			return PathBlocked
		}
		return PathOpen
	case tagged("minecraft:trapdoors") || name == "lily_pad" || name == "big_dripleaf":
		return PathTrapdoor
	case name == "powder_snow":
		return PathPowderSnow
	case name == "cactus" || name == "sweet_berry_bush":
		return PathDamaging
	case name == "honey_block":
		return PathStickyHoney
	case name == "cocoa":
		return PathCocoa
	case name == "wither_rose" || tagged("minecraft:speleothems"):
		return PathDamageCautious
	case lava:
		return PathLava
	case tagged("minecraft:fire") || name == "magma_block" || name == "lava_cauldron" || tagged("minecraft:campfires") && props["lit"] == true:
		return PathFire
	case tagged("minecraft:doors"):
		if props["open"] == true {
			return PathDoorOpen
		}
		if name == "iron_door" {
			return PathDoorIronClosed
		}
		return PathDoorWoodClosed
	case tagged("minecraft:rails"):
		return PathRail
	case tagged("minecraft:leaves"):
		return PathLeaves
	case tagged("minecraft:fences") || tagged("minecraft:walls") || tagged("minecraft:fence_gates") && props["open"] != true:
		return PathFence
	case !pathfindableLandState(tx, pos, b, name, props, tagged):
		return PathBlocked
	case water:
		return PathWater
	}
	return PathOpen
}

// pathfindableLand reports if a land mob may walk through a block, as
// BlockState.isPathfindable(PathComputationType.LAND).
func pathfindableLand(tx pathWorld, pos cube.Pos, b world.Block) bool {
	state, err := world.CanonicalStateOf(b)
	if err != nil {
		return len(b.Model().BBox(pos, tx)) == 0
	}
	name := strings.TrimPrefix(state.Name, "minecraft:")
	return pathfindableLandState(tx, pos, b, name, state.Properties, func(tag string) bool {
		in, _ := gamedata.BlockTagContains(tag, state.Name)
		return in
	})
}

// pathfindableLandState ports the isPathfindable overrides of the blocks of
// Java for land: the blocks a mob never paths through although they don't
// fill their block, and the default of blocks without a full collision box.
func pathfindableLandState(tx pathWorld, pos cube.Pos, b world.Block, name string, props map[string]any, tagged func(string) bool) bool {
	switch {
	case tagged("minecraft:doors") || tagged("minecraft:fence_gates") || tagged("minecraft:trapdoors"):
		return props["open"] == true
	case name == "water" || name == "powder_snow":
		return true
	case name == "lava":
		return false
	case name == "snow":
		layers, _ := props["layers"].(int32)
		return layers < 5
	case tagged("minecraft:beds") || tagged("minecraft:cauldrons") || tagged("minecraft:anvil") || tagged("minecraft:candle_cakes") ||
		tagged("minecraft:campfires") || tagged("minecraft:fences") || tagged("minecraft:walls") || tagged("minecraft:slabs") ||
		tagged("minecraft:stairs") || tagged("minecraft:flower_pots") || tagged("minecraft:speleothems"):
		return false
	case strings.HasSuffix(name, "_skull") || strings.HasSuffix(name, "_head") || strings.HasSuffix(name, "chain") ||
		strings.HasSuffix(name, "chest") || strings.HasSuffix(name, "lantern") || strings.HasSuffix(name, "glass_pane") ||
		strings.HasSuffix(name, "_bars") || strings.HasSuffix(name, "lightning_rod") || strings.HasSuffix(name, "copper_golem_statue") ||
		strings.HasSuffix(name, "_shelf") || strings.HasSuffix(name, "_wall_hanging_sign") || strings.HasSuffix(name, "sculk_sensor"):
		return false
	}
	switch name {
	case "azalea", "flowering_azalea", "bamboo", "bell", "brewing_stand", "cactus", "cake", "chorus_plant", "cocoa", "composter",
		"conduit", "decorated_pot", "dragon_egg", "enchanting_table", "end_portal_frame", "farmland", "grindstone", "heavy_core",
		"hopper", "lectern", "mud", "dirt_path", "respawn_anchor", "end_rod", "sea_pickle", "soul_sand", "stonecutter", "iron_bars":
		return false
	}
	return !fullCollisionCube(tx, pos, b)
}

// fullCollisionCube reports if the collision shape of a block fills it.
func fullCollisionCube(tx world.BlockSource, pos cube.Pos, b world.Block) bool {
	boxes := b.Model().BBox(pos, tx)
	if len(boxes) != 1 {
		return false
	}
	bb := boxes[0]
	return bb.Min() == (mgl64.Vec3{}) && bb.Max() == (mgl64.Vec3{1, 1, 1})
}

// isAir reports if a block is air.
func isAir(b world.Block) bool {
	_, ok := b.(block.Air)
	return ok
}

// pathWater reports if a position holds water.
func pathWater(tx pathWorld, pos cube.Pos) bool {
	l, ok := tx.Liquid(pos)
	return ok && l.LiquidType() == "water"
}

// maxFallDistance ports Mob.getMaxFallDistance: how far a mob drops down a
// path, further when it chases a target with health to spare.
func (m *Mob) maxFallDistance() int {
	if m.Target() == nil {
		return 3
	}
	l, ok := m.Self.(Living)
	if !ok {
		return 3
	}
	sacrifice := int(l.Health() - l.MaxHealth()*.33)
	difficulty, _ := world.DifficultyID(m.tx.World().Difficulty())
	sacrifice -= (3 - difficulty) * 4
	return 3 + max(sacrifice, 0)
}
