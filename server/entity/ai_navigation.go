package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// PathNavigation finds and follows the paths of a walking mob, ported from
// Java's PathNavigation and GroundPathNavigation, or of a swimming or flying
// one.
type PathNavigation struct {
	eval walkNodeEvaluator

	path                      *mobPath
	speedModifier             float64
	tick                      int
	lastStuckCheck            int
	lastStuckCheckPos         mgl64.Vec3
	timeoutCachedNode         cube.Pos
	timeoutTimer              int64
	lastTimeoutCheck          int64
	timeoutLimit              float64
	maxDistanceToWaypoint     float64
	hasDelayedRecomputation   bool
	timeLastRecompute         int64
	targetPos                 *cube.Pos
	reachRange                int
	maxVisitedNodesMultiplier float64
	stuck                     bool
	requiredPathLength        float64
	maxVisitedNodes           int

	// AvoidSun stops paths where they leave the shade, as zombies and
	// skeletons avoid the sun by day.
	AvoidSun bool
	// CanPathToTargetsBelowSurface keeps targets inside blocks where they
	// are instead of moving them up to the surface.
	CanPathToTargetsBelowSurface bool
	// StableDestination, if set, replaces IsStableDestination, as the
	// navigation of a bee overrides isStableDestination.
	StableDestination func(tx *world.Tx, pos cube.Pos) bool
	// Paused, if set, stops the navigation from ticking while it reports
	// true, as a bee pollinating does.
	Paused func() bool

	// wallClimber selects the navigation of Java's WallClimberNavigation:
	// without a path, the mob walks straight at the position it wanted to
	// reach and climbs what is in the way.
	wallClimber    bool
	pathToPosition *cube.Pos
	// amphibious selects the navigation of Java's AmphibiousPathNavigation:
	// the mob walks, and swims through water in every direction.
	amphibious bool
	// swim selects the navigation of Java's WaterBoundPathNavigation: the
	// mob only swims.
	swim *swimNodeEvaluator
}

func (n *PathNavigation) init(m *Mob) {
	n.eval.canPassDoors = true
	n.maxVisitedNodesMultiplier = 1
	n.requiredPathLength = 16
	n.maxDistanceToWaypoint = .5
	n.maxVisitedNodes = int(math.Floor(m.AI.FollowRange * 16))
}

// SetAmphibious makes the mob walk and swim as Java's
// AmphibiousPathNavigation moves it.
func (n *PathNavigation) SetAmphibious(v bool) { n.amphibious, n.eval.amphibious = v, v }

// SetWaterBound makes the mob only swim, as Java's WaterBoundPathNavigation
// moves it. A mob that breaches leaps out of the water.
func (n *PathNavigation) SetWaterBound(breaching bool) {
	n.swim = &swimNodeEvaluator{allowBreaching: breaching}
}

// SetFlying makes the mob fly through the air, as Java's FlyingPathNavigation
// moves it.
func (n *PathNavigation) SetFlying() { n.eval.fly = true }

// SetCanFloat sets whether paths go through water the mob swims across.
func (n *PathNavigation) SetCanFloat(v bool) { n.eval.canFloat = v }

// CanFloat reports if paths go through water the mob swims across.
func (n *PathNavigation) CanFloat() bool { return n.eval.canFloat }

// SetCanOpenDoors sets whether paths go through closed wooden doors.
func (n *PathNavigation) SetCanOpenDoors(v bool) { n.eval.canOpenDoors = v }

// SetCanPassDoors sets whether paths go through open doors.
func (n *PathNavigation) SetCanPassDoors(v bool) { n.eval.canPassDoors = v }

// SetCanWalkOverFences sets whether paths jump over fences.
func (n *PathNavigation) SetCanWalkOverFences(v bool) { n.eval.canWalkOverFences = v }

// SetMaxVisitedNodesMultiplier scales the nodes a path finder visits at
// most, as PathNavigation.setMaxVisitedNodesMultiplier.
func (n *PathNavigation) SetMaxVisitedNodesMultiplier(v float64) { n.maxVisitedNodesMultiplier = v }

// ResetMaxVisitedNodesMultiplier ports
// PathNavigation.resetMaxVisitedNodesMultiplier.
func (n *PathNavigation) ResetMaxVisitedNodesMultiplier() { n.maxVisitedNodesMultiplier = 1 }

// SetSpeedModifier sets the modifier of the speed the path is followed with.
func (n *PathNavigation) SetSpeedModifier(v float64) { n.speedModifier = v }

// SetRequiredPathLength sets the least length paths may have.
func (n *PathNavigation) SetRequiredPathLength(m *Mob, length float64) {
	n.requiredPathLength = length
	n.maxVisitedNodes = int(math.Floor(n.maxPathLength(m) * 16))
}

func (n *PathNavigation) maxPathLength(m *Mob) float64 {
	return math.Max(m.AI.FollowRange, n.requiredPathLength)
}

// Path returns the path the mob follows, or nil.
func (n *PathNavigation) Path() *mobPath { return n.path }

// TargetPos returns the position the last path was found to.
func (n *PathNavigation) TargetPos() (cube.Pos, bool) {
	if n.targetPos == nil {
		return cube.Pos{}, false
	}
	return *n.targetPos, true
}

// IsDone reports if the mob follows no path.
func (n *PathNavigation) IsDone() bool { return n.path == nil || n.path.Done() }

// InProgress reports if the mob follows a path.
func (n *PathNavigation) InProgress() bool { return !n.IsDone() }

// IsStuck reports if the mob got stuck following its last path.
func (n *PathNavigation) IsStuck() bool { return n.stuck }

// Stop stops following the path.
func (n *PathNavigation) Stop() { n.path = nil }

// RecomputePath finds the path to the last target again, at most once a
// second.
func (n *PathNavigation) RecomputePath(m *Mob) {
	if m.GameTime()-n.timeLastRecompute <= 20 || !n.canUpdatePath(m) {
		n.hasDelayedRecomputation = true
		return
	}
	if n.targetPos != nil {
		n.path = nil
		n.path = n.CreatePath(m, *n.targetPos, n.reachRange)
		n.timeLastRecompute = m.GameTime()
		n.hasDelayedRecomputation = false
	}
}

// CreatePath finds a path to a position, ported from
// GroundPathNavigation.createPath(BlockPos, int).
func (n *PathNavigation) CreatePath(m *Mob, pos cube.Pos, reachRange int) *mobPath {
	if n.wallClimber {
		target := pos
		n.pathToPosition = &target
	}
	if n.eval.fly {
		// FlyingPathNavigation flies to the very block passed.
		return n.createPath(m, []cube.Pos{pos}, reachRange, n.maxPathLength(m))
	}
	if _, ok := m.tx.BlockLoaded(pos); !ok && !pos.OutOfBounds(m.tx.Range()) {
		return nil
	}
	if !n.CanPathToTargetsBelowSurface {
		pos = surfacePosition(m.tx, pos)
	}
	return n.createPath(m, []cube.Pos{pos}, reachRange, n.maxPathLength(m))
}

// CreatePathWithLength finds a path to a position no longer than
// maxPathLength.
func (n *PathNavigation) CreatePathWithLength(m *Mob, pos cube.Pos, reachRange int, maxPathLength float64) *mobPath {
	return n.createPath(m, []cube.Pos{pos}, reachRange, maxPathLength)
}

// CreatePathToEntity finds a path to the block an entity stands in.
func (n *PathNavigation) CreatePathToEntity(m *Mob, e world.Entity, reachRange int) *mobPath {
	return n.CreatePath(m, cube.PosFromVec3(e.Position()), reachRange)
}

// CreatePathToAny finds a path to the nearest of positions.
func (n *PathNavigation) CreatePathToAny(m *Mob, targets []cube.Pos, reachRange int) *mobPath {
	return n.createPath(m, targets, reachRange, n.maxPathLength(m))
}

func (n *PathNavigation) createPath(m *Mob, targets []cube.Pos, reachRange int, maxPathLength float64) *mobPath {
	if len(targets) == 0 || m.data.Pos[1] < float64(m.tx.Range().Min()) || !n.canUpdatePath(m) {
		return nil
	}
	if n.path != nil && !n.path.Done() && n.targetPos != nil {
		for _, t := range targets {
			if t == *n.targetPos {
				return n.path
			}
		}
	}
	visited := int(float64(n.maxVisitedNodes) * n.maxVisitedNodesMultiplier)
	var eval nodeEvaluator = &n.eval
	switch {
	case n.swim != nil:
		eval = n.swim
	case n.eval.fly:
		eval = flyNodeEvaluator{&n.eval}
	}
	path := findPath(m, eval, targets, float32(maxPathLength), reachRange, visited)
	if path != nil {
		target := path.Target()
		n.targetPos = &target
		n.reachRange = reachRange
		n.resetStuckTimeout()
	}
	return path
}

// MoveTo follows a path with the speed of the mob scaled by speedModifier
// and reports if there is something left to follow.
func (n *PathNavigation) MoveTo(m *Mob, path *mobPath, speedModifier float64) bool {
	if path == nil {
		n.path = nil
		return false
	}
	if !path.sameAs(n.path) {
		n.path = path
	}
	if n.IsDone() {
		return false
	}
	n.trimPath(m)
	if n.path.NodeCount() <= 0 {
		return false
	}
	n.speedModifier = speedModifier
	n.lastStuckCheck = n.tick
	n.lastStuckCheckPos = n.tempMobPos(m)
	return true
}

// MoveToPos finds and follows a path to a position.
func (n *PathNavigation) MoveToPos(m *Mob, x, y, z float64, speedModifier float64) bool {
	return n.MoveTo(m, n.CreatePath(m, cube.PosFromVec3(mgl64.Vec3{x, y, z}), 1), speedModifier)
}

// MoveToPosReach finds and follows a path to within reachRange of a
// position.
func (n *PathNavigation) MoveToPosReach(m *Mob, x, y, z float64, reachRange int, speedModifier float64) bool {
	return n.MoveTo(m, n.CreatePath(m, cube.PosFromVec3(mgl64.Vec3{x, y, z}), reachRange), speedModifier)
}

// MoveToEntity finds and follows a path to an entity.
func (n *PathNavigation) MoveToEntity(m *Mob, e world.Entity, speedModifier float64) bool {
	if n.wallClimber {
		if path := n.CreatePathToEntity(m, e, 0); path != nil {
			return n.MoveTo(m, path, speedModifier)
		}
		target := cube.PosFromVec3(e.Position())
		n.pathToPosition, n.speedModifier = &target, speedModifier
		return true
	}
	return n.MoveToEntityReach(m, e, 1, speedModifier)
}

// MoveToEntityReach finds and follows a path to within reachRange of an
// entity.
func (n *PathNavigation) MoveToEntityReach(m *Mob, e world.Entity, reachRange int, speedModifier float64) bool {
	path := n.CreatePathToEntity(m, e, reachRange)
	return path != nil && n.MoveTo(m, path, speedModifier)
}

// tickNavigation ports PathNavigation.tick.
func (n *PathNavigation) tickNavigation(m *Mob) {
	if n.Paused != nil && n.Paused() {
		return
	}
	if n.wallClimber && n.IsDone() {
		n.tickWallClimber(m)
		return
	}
	n.tick++
	if n.hasDelayedRecomputation {
		n.RecomputePath(m)
	}
	if n.IsDone() {
		return
	}
	if n.canUpdatePath(m) {
		n.followThePath(m)
	} else if n.eval.fly && n.path != nil && !n.path.Done() {
		// FlyingPathNavigation.tick: a mob that may not follow its path
		// passes the nodes it flies through.
		next := n.path.entityPosAtNode(m.Width(), n.path.next)
		if m.BlockPos() == cube.PosFromVec3(next) {
			n.path.Advance()
		}
	} else if n.path != nil && !n.path.Done() {
		pos := n.tempMobPos(m)
		next := n.path.entityPosAtNode(m.Width(), n.path.next)
		if pos[1] > next[1] && !m.OnGround() && math.Floor(pos[0]) == math.Floor(next[0]) && math.Floor(pos[2]) == math.Floor(next[2]) {
			n.path.Advance()
		}
	}
	if !n.IsDone() {
		target := n.path.entityPosAtNode(m.Width(), n.path.next)
		m.AI.move.SetWantedPosition(target[0], n.groundY(m, target), target[2], n.speedModifier)
	}
}

// tickWallClimber ports the tick of WallClimberNavigation without a path.
func (n *PathNavigation) tickWallClimber(m *Mob) {
	if n.pathToPosition == nil {
		return
	}
	p := *n.pathToPosition
	width := m.Width()
	near := func(pos cube.Pos) bool { return pos.Vec3Centre().Sub(m.data.Pos).LenSqr() < width*width }
	column := cube.PosFromVec3(mgl64.Vec3{float64(p.X()), m.data.Pos[1], float64(p.Z())})
	if !near(p) && (m.data.Pos[1] <= float64(p.Y()) || !near(column)) {
		m.AI.move.SetWantedPosition(float64(p.X()), float64(p.Y()), float64(p.Z()), n.speedModifier)
		return
	}
	n.pathToPosition = nil
}

func (n *PathNavigation) groundY(m *Mob, target mgl64.Vec3) float64 {
	if n.amphibious || n.swim != nil || n.eval.fly {
		return target[1]
	}
	pos := cube.PosFromVec3(target)
	if isAir(pathBlockAt(m.tx, pos.Side(cube.FaceDown))) {
		return target[1]
	}
	return pathFloorLevel(m.tx, pos)
}

// followThePath ports PathNavigation.followThePath.
func (n *PathNavigation) followThePath(m *Mob) {
	pos := n.tempMobPos(m)
	width := m.Width()
	if width > .75 {
		n.maxDistanceToWaypoint = width / 2
	} else {
		n.maxDistanceToWaypoint = .75 - width/2
	}
	node := n.path.nextNodePos()
	xd := math.Abs(m.data.Pos[0] - (float64(node.X()) + .5))
	yd := math.Abs(m.data.Pos[1] - float64(node.Y()))
	zd := math.Abs(m.data.Pos[2] - (float64(node.Z()) + .5))
	// PathNavigation.getMaxVerticalDistanceToWaypoint, half a block for
	// swimmers.
	maxYd := 1.0
	if n.swim != nil {
		maxYd = .5
	}
	close := xd < n.maxDistanceToWaypoint && zd < n.maxDistanceToWaypoint && yd < maxYd
	if close || canCutCorner(n.path.nodes[n.path.next].typ) && n.shouldTargetNextNode(m, pos) {
		n.path.Advance()
	}
	n.doStuckDetection(m, pos)
}

func canCutCorner(t PathType) bool {
	return t != PathFireInNeighbour && t != PathDamagingInNeighbour && t != PathWalkableDoor
}

// shouldTargetNextNode ports PathNavigation.shouldTargetNextNodeInDirection.
func (n *PathNavigation) shouldTargetNextNode(m *Mob, pos mgl64.Vec3) bool {
	if n.path.next+1 >= n.path.NodeCount() {
		return false
	}
	current := n.path.nextNodePos().Vec3Middle()
	current[1] = float64(n.path.nextNodePos().Y())
	if pos.Sub(current).Len() >= 2 {
		return false
	}
	if n.canMoveDirectly(m, pos, n.path.entityPosAtNode(m.Width(), n.path.next)) {
		return true
	}
	nextPos := n.path.nodePos(n.path.next + 1)
	next := mgl64.Vec3{float64(nextPos.X()) + .5, float64(nextPos.Y()), float64(nextPos.Z()) + .5}
	toCurrent, toNext := current.Sub(pos), next.Sub(pos)
	currentSqr, nextSqr := toCurrent.LenSqr(), toNext.LenSqr()
	if nextSqr >= currentSqr && currentSqr >= .5 {
		return false
	}
	if currentSqr == 0 || nextSqr == 0 {
		return false
	}
	return toNext.Normalize().Dot(toCurrent.Normalize()) < 0
}

// doStuckDetection ports PathNavigation.doStuckDetection.
func (n *PathNavigation) doStuckDetection(m *Mob, pos mgl64.Vec3) {
	if n.tick-n.lastStuckCheck > 100 {
		speed := m.Speed()
		if speed < 1 {
			speed *= speed
		}
		threshold := speed * 100 * .25
		n.stuck = pos.Sub(n.lastStuckCheckPos).LenSqr() < threshold*threshold
		if n.stuck {
			n.Stop()
		}
		n.lastStuckCheck = n.tick
		n.lastStuckCheckPos = pos
	}
	if n.path == nil || n.path.Done() {
		return
	}
	node := n.path.nextNodePos()
	time := m.GameTime()
	if node == n.timeoutCachedNode {
		n.timeoutTimer += time - n.lastTimeoutCheck
	} else {
		n.timeoutCachedNode = node
		dist := pos.Sub(mgl64.Vec3{float64(node.X()) + .5, float64(node.Y()), float64(node.Z()) + .5}).Len()
		n.timeoutLimit = 0
		if m.Speed() > 0 {
			n.timeoutLimit = dist / m.Speed() * 20
		}
	}
	if n.timeoutLimit > 0 && float64(n.timeoutTimer) > n.timeoutLimit*3 {
		n.resetStuckTimeout()
		n.Stop()
	}
	n.lastTimeoutCheck = time
}

func (n *PathNavigation) resetStuckTimeout() {
	n.timeoutCachedNode = cube.Pos{}
	n.timeoutTimer, n.timeoutLimit = 0, 0
	n.stuck = false
}

// trimPath ports PathNavigation.trimPath and that of GroundPathNavigation:
// paths step over cauldrons and end where the shade ends for mobs that
// avoid the sun.
func (n *PathNavigation) trimPath(m *Mob) {
	if n.path == nil {
		return
	}
	for i, node := range n.path.nodes {
		if !blockInTag(pathBlockAt(m.tx, node.Pos()), "minecraft:cauldrons") {
			continue
		}
		n.path.nodes[i] = node.cloneAndMove(node.x, node.y+1, node.z)
		if i+1 < len(n.path.nodes) && node.y >= n.path.nodes[i+1].y {
			next := n.path.nodes[i+1]
			n.path.nodes[i+1] = node.cloneAndMove(next.x, node.y+1, next.z)
		}
	}
	if !n.AvoidSun || canSeeSky(m.tx, cube.PosFromVec3(m.data.Pos.Add(mgl64.Vec3{0, .5}))) {
		return
	}
	for i, node := range n.path.nodes {
		if canSeeSky(m.tx, node.Pos()) {
			n.path.truncate(i)
			return
		}
	}
}

// canSeeSky reports if a position sees the sky, as Level.canSeeSky.
func canSeeSky(tx *world.Tx, pos cube.Pos) bool {
	return tx.SkyLight(pos) >= 15
}

// canUpdatePath reports if the mob may find a new path: when it stands,
// swims or rides.
func (n *PathNavigation) canUpdatePath(m *Mob) bool {
	if n.swim != nil {
		return n.swim.allowBreaching || m.InLiquid()
	}
	if n.eval.fly {
		return n.eval.canFloat && m.InLiquid() || m.Riding() == nil
	}
	return n.amphibious || m.OnGround() || m.InLiquid() || m.Riding() != nil
}

// canMoveDirectly ports PathNavigation.canMoveDirectly: a swimming
// amphibious mob heads straight for the next node it sees, and a flying mob
// for one it sees past blocks and liquids.
func (n *PathNavigation) canMoveDirectly(m *Mob, from, to mgl64.Vec3) bool {
	if n.eval.fly {
		return clipMissFluids(m.tx, from, mgl64.Vec3{to[0], to[1] + m.Height()*.5, to[2]})
	}
	if n.swim == nil && (!n.amphibious || !m.InLiquid()) {
		return false
	}
	return clipMiss(m.tx, from, mgl64.Vec3{to[0], to[1] + m.Height()*.5, to[2]})
}

// tempMobPos ports GroundPathNavigation.getTempMobPos, that of
// AmphibiousPathNavigation, the middle of the mob, and that of
// FlyingPathNavigation, its feet.
func (n *PathNavigation) tempMobPos(m *Mob) mgl64.Vec3 {
	if n.eval.fly {
		return m.data.Pos
	}
	if n.amphibious || n.swim != nil {
		return mgl64.Vec3{m.data.Pos[0], m.data.Pos[1] + m.Height()*.5, m.data.Pos[2]}
	}
	return mgl64.Vec3{m.data.Pos[0], float64(n.surfaceY(m)), m.data.Pos[2]}
}

func (n *PathNavigation) surfaceY(m *Mob) int {
	if m.InWater() && n.eval.canFloat {
		x, z := int(math.Floor(m.data.Pos[0])), int(math.Floor(m.data.Pos[2]))
		surface := int(math.Floor(m.data.Pos[1]))
		for steps := 0; pathWater(m.tx, cube.Pos{x, surface, z}); steps++ {
			if steps > 16 {
				return int(math.Floor(m.data.Pos[1]))
			}
			surface++
		}
		return surface
	}
	return int(math.Floor(m.data.Pos[1] + .5))
}

// IsStableDestination reports if a mob may stand at a position: on a full,
// solid block, as PathNavigation.isStableDestination.
func (n *PathNavigation) IsStableDestination(tx *world.Tx, pos cube.Pos) bool {
	if n.StableDestination != nil {
		return n.StableDestination(tx, pos)
	}
	if n.eval.fly {
		// FlyingPathNavigation: a block with a full top to land on.
		return pathBlockAt(tx, pos).Model().FaceSolid(pos, cube.FaceUp, tx)
	}
	if n.swim != nil {
		b := pathBlockAt(tx, pos)
		return !fullCollisionCube(tx, pos, b)
	}
	below := pos.Side(cube.FaceDown)
	b := pathBlockAt(tx, below)
	if n.amphibious {
		return !isAir(b)
	}
	return fullCollisionCube(tx, below, b) && b.Model().FaceSolid(below, cube.FaceUp, tx)
}

// ShouldRecomputePath reports if a block change at pos may change the path,
// as PathNavigation.shouldRecomputePath.
func (n *PathNavigation) ShouldRecomputePath(m *Mob, pos cube.Pos) bool {
	if n.hasDelayedRecomputation || n.path == nil || n.path.Done() || n.path.NodeCount() == 0 {
		return false
	}
	end := n.path.EndNode()
	middle := mgl64.Vec3{(float64(end.x) + m.data.Pos[0]) / 2, (float64(end.y) + m.data.Pos[1]) / 2, (float64(end.z) + m.data.Pos[2]) / 2}
	r := float64(n.path.NodeCount() - n.path.next)
	return pos.Vec3Centre().Sub(middle).LenSqr() < r*r
}

// surfacePosition ports GroundPathNavigation.findSurfacePosition: a target
// in the air drops to the ground below it, and one inside blocks rises to
// the top of them.
func surfacePosition(tx *world.Tx, pos cube.Pos) cube.Pos {
	r := tx.Range()
	if isAir(pathBlockAt(tx, pos)) {
		column := pos.Side(cube.FaceDown)
		for column.Y() >= r.Min() && isAir(pathBlockAt(tx, column)) {
			column = column.Side(cube.FaceDown)
		}
		if column.Y() >= r.Min() {
			return column.Side(cube.FaceUp)
		}
		column = cube.Pos{pos.X(), pos.Y() + 1, pos.Z()}
		for column.Y() <= r.Max() && isAir(pathBlockAt(tx, column)) {
			column = column.Side(cube.FaceUp)
		}
		pos = column
	}
	if !legacySolid(tx, pathBlockAt(tx, pos), pos) {
		return pos
	}
	column := pos.Side(cube.FaceUp)
	for column.Y() <= r.Max() && legacySolid(tx, pathBlockAt(tx, column), column) {
		column = column.Side(cube.FaceUp)
	}
	return column
}
