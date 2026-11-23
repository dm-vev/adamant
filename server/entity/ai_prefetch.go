package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Prefetch implements world.Prefetcher. In the parallel phase before the
// entities of a world tick, a mob reads what its tick is going to read from
// the world as it stands: the fluids around its box and the collision boxes
// of the blocks it may move through. The tick uses a result only while the
// box it was read for and the block versions of the columns it read still
// match, so it gets exactly what reading the world itself would give.
func (e *Ent) Prefetch(self world.Entity, v *world.ReadView) {
	if ai, _ := e.mobAI(); ai != nil {
		ai.prefetch(e, self, v)
	}
}

var _ world.Prefetcher = (*Ent)(nil)

// verifyPrefetch makes ticks that use a prefetched result read the world
// as well and panic if the two differ, counting the checks in
// verifiedFluids and verifiedCollisions. Tests set it.
var (
	verifyPrefetch                                    bool
	verifiedFluids, verifiedCollisions, verifiedPaths int
)

// prefetchColumns is the most columns a prefetched result depends on.
const prefetchColumns = 9

// columnVersions are the block versions of the columns a prefetched result
// was read from.
type columnVersions struct {
	n   int
	pos [prefetchColumns]world.ChunkPos
	ver [prefetchColumns]uint64
}

// record notes the versions of the columns under the blocks from lo to hi,
// inclusive. It fails if one is not readable or there are too many.
func (c *columnVersions) record(v *world.ReadView, lo, hi cube.Pos) bool {
	c.n = 0
	for cx := int32(lo[0]) >> 4; cx <= int32(hi[0])>>4; cx++ {
		for cz := int32(lo[2]) >> 4; cz <= int32(hi[2])>>4; cz++ {
			if c.n == prefetchColumns {
				return false
			}
			ver, ok := v.ColumnBlockVersion(world.ChunkPos{cx, cz})
			if !ok {
				return false
			}
			c.pos[c.n], c.ver[c.n] = world.ChunkPos{cx, cz}, ver
			c.n++
		}
	}
	return true
}

// current reports whether none of the columns changed since record.
func (c *columnVersions) current(tx *world.Tx) bool {
	for i := range c.n {
		if ver, ok := tx.ColumnBlockVersion(c.pos[i]); !ok || ver != c.ver[i] {
			return false
		}
	}
	return true
}

// mobPrefetch holds what the parallel phase read for a mob.
type mobPrefetch struct {
	fluidOK   bool
	fluidBox  cube.BBox
	fluid     fluidContact
	fluidCols columnVersions

	collisions collisionPrefetch
	path       pathPrefetch
}

// collisionPrefetch holds the collision boxes of the blocks from lo up to hi,
// exclusive, in the order blockBBoxsAround visits them.
type collisionPrefetch struct {
	ok     bool
	lo, hi cube.Pos
	cells  []collisionCell
	boxes  []cube.BBox
	cols   columnVersions
}

// collisionCell is a block with collision boxes; its boxes, moved to its
// position, are boxes[from:to] of the prefetch.
type collisionCell struct {
	pos      cube.Pos
	from, to int32
}

// prefetch reads the fluids and collision boxes around the mob for its next
// tick.
func (ai *MobAI) prefetch(e *Ent, self world.Entity, v *world.ReadView) {
	p := &ai.pre
	p.fluidOK, p.collisions.ok = false, false

	vel := e.data.Vel
	stationary := vel[0] == 0 && vel[2] == 0 && (vel[1] == 0 || ai.onGround) && !ai.nav.InProgress() && ai.xxa == 0 && ai.zza == 0
	needFluid := !stationary || ai.waterHeight > 0 || ai.lavaHeight > 0
	if needFluid || !stationary {
		box := self.H().Type().BBox(self).Translate(e.data.Pos)
		if needFluid {
			// updateFluids measures the fluids around the box the mob starts
			// its tick with. Only prefetch fluids when moving or already in
			// liquid and surrounding subchunks may contain liquid; otherwise
			// the owner's fast MayContainLiquid check is cheaper.
			shrunk := box.Grow(-.001)
			minX, minY, minZ := int(math.Floor(shrunk.Min()[0])), int(math.Floor(shrunk.Min()[1])), int(math.Floor(shrunk.Min()[2]))
			maxX, maxY, maxZ := int(math.Ceil(shrunk.Max()[0]))-1, int(math.Ceil(shrunk.Max()[1]))-1, int(math.Ceil(shrunk.Max()[2]))-1
			fLo, fHi := cube.Pos{minX, minY, minZ}, cube.Pos{maxX, maxY, maxZ}
			v.Reset()
			if v.MayContainLiquid(fLo, fHi) {
				fluid := measureFluidsRange(v, box.Min()[1], shrunk.Min()[1], minX, minY, minZ, maxX, maxY, maxZ, true)
				lo, hi := fLo, fHi
				if fluid.waterHeight > 0 || fluid.lavaHeight > 0 {
					lo, hi = fluidReadRange(box)
				}
				if !v.Missed() && p.fluidCols.record(v, lo, hi) {
					p.fluid, p.fluidBox, p.fluidOK = fluid, box, true
				}
			}
		}

		// Only prefetch collision boxes when the mob is moving or navigating.
		if !stationary {
			horizontal := .55 + math.Max(math.Abs(vel[0]), math.Abs(vel[2]))
			down := .40 + math.Abs(vel[1])
			step := ai.StepHeight
			if ai.ridden {
				step = math.Max(step, 1)
			}
			up := math.Max(step, .45) + .35 + math.Abs(vel[1])
			area := cube.Box(box.Min()[0]-horizontal, box.Min()[1]-down, box.Min()[2]-horizontal, box.Max()[0]+horizontal, box.Max()[1]+up, box.Max()[2]+horizontal)
			v.Reset()
			p.collisions.read(v, area)
		}
	}

	ai.prefetchPath(e, self, v)
}

// fluidReadRange returns the blocks measureFluids may read for box: those
// it measures and their neighbours, whose liquid it compares.
func fluidReadRange(box cube.BBox) (cube.Pos, cube.Pos) {
	lo, hi := box.Min(), box.Max()
	return cube.Pos{int(math.Floor(lo[0])) - 1, int(math.Floor(lo[1])) - 1, int(math.Floor(lo[2])) - 1},
		cube.Pos{int(math.Ceil(hi[0])) + 1, int(math.Ceil(hi[1])) + 1, int(math.Ceil(hi[2])) + 1}
}

// read reads the collision boxes of the blocks area spans, as
// blockBBoxsAround would for area.
func (c *collisionPrefetch) read(v *world.ReadView, area cube.BBox) {
	c.ok = false
	c.cells, c.boxes = c.cells[:0], c.boxes[:0]
	lo, hi := area.Min(), area.Max()
	c.lo = cube.Pos{int(math.Floor(lo[0])), int(math.Floor(lo[1])), int(math.Floor(lo[2]))}
	c.hi = cube.Pos{int(math.Ceil(hi[0])), int(math.Ceil(hi[1])), int(math.Ceil(hi[2]))}
	reg := v.World().BlockRegistry()
	rng := v.Range()
	yStart := max(c.lo[1], rng[0])
	yEnd := min(c.hi[1], rng[1]+1)
	for y := yStart; y < yEnd; y++ {
		fy := float64(y)
		for x := c.lo[0]; x < c.hi[0]; x++ {
			fx := float64(x)
			for z := c.lo[2]; z < c.hi[2]; z++ {
				pos := cube.Pos{x, y, z}
				rid, ok := v.BlockRID(pos)
				if !ok {
					return
				}
				flags := reg.BlockFlags(rid)
				if flags.EmptyCollision() {
					continue
				}
				from := int32(len(c.boxes))
				if flags.FullCubeCollision() {
					fz := float64(z)
					c.boxes = append(c.boxes, cube.Box(fx, fy, fz, fx+1, fy+1, fz+1))
					c.cells = append(c.cells, collisionCell{pos: pos, from: from, to: int32(len(c.boxes))})
					continue
				}
				boxes := v.Block(pos).Model().BBox(pos, v)
				if len(boxes) == 0 {
					continue
				}
				offset := mgl64.Vec3{fx, fy, float64(z)}
				for _, bb := range boxes {
					c.boxes = append(c.boxes, bb.Translate(offset))
				}
				c.cells = append(c.cells, collisionCell{pos: pos, from: from, to: int32(len(c.boxes))})
			}
		}
	}
	// Models of a block may look at the blocks beside it.
	if !v.Missed() && c.cols.record(v, c.lo.Sub(cube.Pos{1, 1, 1}), c.hi) {
		c.ok = true
	}
}

// boxesAround appends what blockBBoxsAround(tx, box, buf) returns from the
// prefetched boxes, if they cover box and are current.
func (c *collisionPrefetch) boxesAround(tx *world.Tx, box cube.BBox, buf []cube.BBox) ([]cube.BBox, bool) {
	if !c.ok {
		return buf, false
	}
	grown := box.Grow(0.25)
	min, max := grown.Min(), grown.Max()
	lo := cube.Pos{int(math.Floor(min[0])), int(math.Floor(min[1])), int(math.Floor(min[2]))}
	hi := cube.Pos{int(math.Ceil(max[0])), int(math.Ceil(max[1])), int(math.Ceil(max[2]))}
	if lo[0] < c.lo[0] || lo[1] < c.lo[1] || lo[2] < c.lo[2] || hi[0] > c.hi[0] || hi[1] > c.hi[1] || hi[2] > c.hi[2] {
		return buf, false
	}
	if !c.cols.current(tx) {
		c.ok = false
		return buf, false
	}
	for _, cell := range c.cells {
		p := cell.pos
		if p[0] < lo[0] || p[0] >= hi[0] || p[1] < lo[1] || p[1] >= hi[1] || p[2] < lo[2] || p[2] >= hi[2] {
			continue
		}
		buf = append(buf, c.boxes[cell.from:cell.to]...)
	}
	return buf, true
}

// prefetchedFluids returns the fluids prefetched for box if they are
// current.
func (p *mobPrefetch) prefetchedFluids(tx *world.Tx, box cube.BBox) (fluidContact, bool) {
	if !p.fluidOK || p.fluidBox != box {
		return fluidContact{}, false
	}
	p.fluidOK = false
	if !p.fluidCols.current(tx) {
		return fluidContact{}, false
	}
	return p.fluid, true
}

// asMeleeAttackGoal returns the goal itself, also through goals embedding it.
func (g *meleeAttackGoal) asMeleeAttackGoal() *meleeAttackGoal { return g }

// pathPrefetch is a path found in the parallel phase for a request the mob
// was expected to make in its tick, with everything the path depends on.
// takePath hands it out only for exactly that request, from the same
// state, while the columns the search read keep their block versions.
type pathPrefetch struct {
	ok            bool
	tick          int64
	path          *mobPath
	target        cube.Pos
	reach         int
	maxPathLength float64
	visited       int
	pos           mgl64.Vec3
	box           cube.BBox
	onGround      bool
	inWater       bool
	step          float64
	maxFall       int
	config        walkNodeConfig
	maluses       map[PathType]float64
	cols          []world.ChunkPos
	versions      []uint64
}

// walkNodeConfig is the configuration of a walkNodeEvaluator a path depends
// on.
type walkNodeConfig struct {
	canPassDoors, canOpenDoors, canFloat, canWalkOverFences, amphibious, prefersShallow, frog, fly bool
}

func (w *walkNodeEvaluator) config() walkNodeConfig {
	return walkNodeConfig{w.canPassDoors, w.canOpenDoors, w.canFloat, w.canWalkOverFences, w.amphibious, w.prefersShallow, w.frog, w.fly}
}

func (ai *MobAI) findMeleeAttackGoal() (*meleeAttackGoal, *wrappedGoal) {
	for _, w := range ai.Goals.goals {
		if g, ok := w.goal.(interface{ asMeleeAttackGoal() *meleeAttackGoal }); ok {
			return g.asMeleeAttackGoal(), w
		}
	}
	return nil, nil
}

func (ai *MobAI) findRangedBowAttackGoal() (*rangedBowAttackGoal, *wrappedGoal) {
	for _, w := range ai.Goals.goals {
		if g, ok := w.goal.(interface{ asRangedBowAttackGoal() *rangedBowAttackGoal }); ok {
			return g.asRangedBowAttackGoal(), w
		}
	}
	return nil, nil
}

func (ai *MobAI) findRangedAttackGoal() (*rangedAttackGoal, *wrappedGoal) {
	for _, w := range ai.Goals.goals {
		if g, ok := w.goal.(interface{ asRangedAttackGoal() *rangedAttackGoal }); ok {
			return g.asRangedAttackGoal(), w
		}
	}
	return nil, nil
}

func (ai *MobAI) meleePrefetchTarget(v *world.ReadView, tick int64, full bool) (cube.Pos, bool) {
	melee, wrapped := ai.findMeleeAttackGoal()
	if melee == nil || ai.target == nil {
		return cube.Pos{}, false
	}
	targetPos, ok := v.EntityPosition(ai.target)
	if !ok {
		return cube.Pos{}, false
	}
	if wrapped.running {
		vel, _ := v.EntityVelocity(ai.target)
		reach := 1 - (math.Hypot(vel[0], vel[2])*2 + .05)
		moving := melee.pathedTarget == (mgl64.Vec3{}) || reach <= 0 || targetPos.Sub(melee.pathedTarget).LenSqr() >= reach*reach
		if melee.ticksUntilNextPathRecalc > 1 || !moving && ai.nav.path != nil && !ai.nav.path.Done() {
			return cube.Pos{}, false
		}
	} else if !full || melee.hasLastCanUseCheck && tick-melee.lastCanUseCheck < 20 || wrapped.flags&ai.Goals.disabled != 0 || !ai.Goals.canReplaceAll(wrapped) {
		return cube.Pos{}, false
	}
	return cube.PosFromVec3(targetPos), true
}

func (ai *MobAI) rangedBowPrefetchTarget(e *Ent, v *world.ReadView, full bool) (cube.Pos, bool) {
	bow, wrapped := ai.findRangedBowAttackGoal()
	if bow == nil || ai.target == nil {
		return cube.Pos{}, false
	}
	u, ok := e.Behaviour().(mobBowUser)
	if !ok || !u.holdingBow() {
		return cube.Pos{}, false
	}
	if !wrapped.running && (!full || wrapped.flags&ai.Goals.disabled != 0 || !ai.Goals.canReplaceAll(wrapped)) {
		return cube.Pos{}, false
	}
	targetPos, ok := v.EntityPosition(ai.target)
	if !ok {
		return cube.Pos{}, false
	}
	if wrapped.running && e.data.Pos.Sub(targetPos).LenSqr() <= bow.attackRadiusSqr && bow.seeTime >= 20 {
		return cube.Pos{}, false
	}
	return cube.PosFromVec3(targetPos), true
}

func (ai *MobAI) rangedPrefetchTarget(e *Ent, v *world.ReadView, full bool) (cube.Pos, bool) {
	ranged, wrapped := ai.findRangedAttackGoal()
	if ranged == nil {
		return cube.Pos{}, false
	}
	targetH := ai.target
	if wrapped.running {
		if ranged.target != nil {
			targetH = ranged.target
		}
	} else if !full || wrapped.flags&ai.Goals.disabled != 0 || !ai.Goals.canReplaceAll(wrapped) {
		return cube.Pos{}, false
	}
	if targetH == nil {
		return cube.Pos{}, false
	}
	targetPos, ok := v.EntityPosition(targetH)
	if !ok {
		return cube.Pos{}, false
	}
	if wrapped.running && e.data.Pos.Sub(targetPos).LenSqr() <= ranged.AttackRadius*ranged.AttackRadius && ranged.seeTime >= 5 {
		return cube.Pos{}, false
	}
	return cube.PosFromVec3(targetPos), true
}

func (ai *MobAI) brainWalkPrefetchTarget(e *Ent, v *world.ReadView) (cube.Pos, bool) {
	if ai.capsBrain == nil {
		return cube.Pos{}, false
	}
	br := ai.capsBrain.brain()
	if br == nil {
		return cube.Pos{}, false
	}
	wtVal, ok := br.get(memWalkTarget)
	if !ok {
		return cube.Pos{}, false
	}
	wt := wtVal.(walkTarget)
	var raw cube.Pos
	switch t := wt.target.(type) {
	case blockPosTracker:
		raw = t.pos
	case *entityTracker:
		pos, ok := v.EntityPosition(t.h)
		if !ok {
			return cube.Pos{}, false
		}
		raw = cube.PosFromVec3(pos)
	default:
		return cube.Pos{}, false
	}
	if !br.has(memPath) && manhattan(raw, cube.PosFromVec3(e.data.Pos)) <= wt.closeEnough {
		return cube.Pos{}, false
	}
	return raw, true
}

func (ai *MobAI) selectPrefetchTarget(e *Ent, v *world.ReadView, tick int64) (cube.Pos, int, bool) {
	next := ai.tickCount + 1
	full := (next+ai.entityID)%2 == 0 || next <= 1
	if pos, ok := ai.meleePrefetchTarget(v, tick, full); ok {
		return pos, 0, true
	}
	if pos, ok := ai.rangedBowPrefetchTarget(e, v, full); ok {
		return pos, 1, true
	}
	if pos, ok := ai.rangedPrefetchTarget(e, v, full); ok {
		return pos, 1, true
	}
	if pos, ok := ai.brainWalkPrefetchTarget(e, v); ok {
		return pos, 0, true
	}
	return cube.Pos{}, 0, false
}

// prefetchPath finds, on a worker, the path the mob's attack goal or brain
// walk target is expected to ask for this tick. Only walking mobs whose paths
// depend on nothing but blocks and their own state are covered.
func (ai *MobAI) prefetchPath(e *Ent, self world.Entity, v *world.ReadView) {
	p := &ai.pre.path
	p.ok, p.path = false, nil
	n := &ai.nav
	if n.vehicle != nil || n.wallClimber || n.swim != nil || n.amphibious || n.eval.fly || n.eval.amphibious || n.eval.frog || n.eval.blocked != nil {
		return
	}
	if !ai.started || ai.caps&mobCapPathfindingListener != 0 {
		// The id that staggers the goals and capability flags are known once the AI has started.
		return
	}
	tick := v.World().CurrentTick()
	target, reach, ok := ai.selectPrefetchTarget(e, v, tick)
	if !ok {
		return
	}
	if _, ok := v.BlockLoaded(target); !ok {
		return
	}
	if !n.CanPathToTargetsBelowSurface {
		target = surfacePosition(v, target)
	}
	if v.Missed() {
		return
	}
	if n.path != nil && !n.path.Done() && n.targetPos != nil && *n.targetPos == target {
		// createPath keeps following the path it has.
		return
	}
	l, living := self.(Living)
	if !living {
		return
	}
	m := &Mob{Ent: e, AI: ai, Self: self}
	maxFall := 3
	if ai.target != nil {
		maxFall = 3 + max(int(l.Health()-l.MaxHealth()*.33)-(3-difficultyID(v.World().Difficulty()))*4, 0)
	}
	visited := int(float64(n.maxVisitedNodes) * n.maxVisitedNodesMultiplier)
	maxPathLength := n.maxPathLength(m)
	if _, backedOff := n.unreachableBackedOff(v, cube.PosFromVec3(e.data.Pos), target, reach, maxPathLength, tick); backedOff {
		return
	}

	v.Reset()
	v.Track()
	ev := n.eval
	ev.pw, ev.maxFall = v, maxFall
	targets := [1]cube.Pos{target}
	path := findPath(m, &ev, targets[:], float32(maxPathLength), reach, visited)
	if v.Missed() {
		return
	}
	p.cols = append(p.cols[:0], v.Tracked()...)
	p.versions = p.versions[:0]
	for _, pos := range p.cols {
		ver, ok := v.ColumnBlockVersion(pos)
		if !ok {
			return
		}
		p.versions = append(p.versions, ver)
	}
	p.ok, p.tick, p.path, p.target, p.reach, p.maxPathLength, p.visited = true, tick, path, target, reach, maxPathLength, visited
	p.pos, p.box, p.onGround, p.inWater, p.step, p.maxFall = e.data.Pos, m.BBox(), ai.onGround, ai.waterHeight > 0, ai.StepHeight, maxFall
	p.config = n.eval.config()
	if len(p.maluses) > 0 {
		clear(p.maluses)
	}
	if len(ai.maluses) > 0 {
		if p.maluses == nil {
			p.maluses = make(map[PathType]float64, len(ai.maluses))
		}
		for t, malus := range ai.maluses {
			p.maluses[t] = malus
		}
	}
}

// takePath returns the prefetched path if the request and everything it
// depends on match what it was found for, consuming it.
func (p *pathPrefetch) takePath(m *Mob, eval nodeEvaluator, targets []cube.Pos, maxPathLength float64, reach, visited int) (*mobPath, bool) {
	if !p.ok {
		return nil, false
	}
	p.ok = false
	n := &m.AI.nav
	if eval != nodeEvaluator(&n.eval) || len(targets) != 1 || targets[0] != p.target || reach != p.reach || maxPathLength != p.maxPathLength ||
		visited != p.visited || p.tick != m.tx.CurrentTick() || m.data.Pos != p.pos || m.BBox() != p.box || m.AI.onGround != p.onGround ||
		m.InWater() != p.inWater || m.AI.StepHeight != p.step || n.eval.config() != p.config || n.eval.blocked != nil || len(m.AI.maluses) != len(p.maluses) {
		return nil, false
	}
	for t, malus := range m.AI.maluses {
		if prefetched, ok := p.maluses[t]; !ok || prefetched != malus {
			return nil, false
		}
	}
	if m.maxFallDistance() != p.maxFall {
		return nil, false
	}
	for i, pos := range p.cols {
		if ver, ok := m.tx.ColumnBlockVersion(pos); !ok || ver != p.versions[i] {
			return nil, false
		}
	}
	path := p.path
	p.path = nil
	return path, true
}

// pathsIdentical reports whether two paths have the same nodes, with the
// same types, costs and distances, and the same target.
func pathsIdentical(a, b *mobPath) bool {
	if a == nil || b == nil {
		return a == b
	}
	if len(a.nodes) != len(b.nodes) || a.target != b.target || a.reached != b.reached || a.distToTarget != b.distToTarget || a.next != b.next {
		return false
	}
	for i, n := range a.nodes {
		o := b.nodes[i]
		if n.x != o.x || n.y != o.y || n.z != o.z || n.typ != o.typ || n.costMalus != o.costMalus || n.walkedDistance != o.walkedDistance || n.g != o.g || n.closed != o.closed {
			return false
		}
	}
	return true
}
