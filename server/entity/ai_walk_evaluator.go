package entity

import (
	"math"
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
	// amphibious selects the evaluation of AmphibiousNodeEvaluator.
	amphibious bool

	m                                      *Mob
	nodes                                  map[int64]*pathNodeData
	types                                  map[int64]PathType
	collisions                             map[cube.BBox]bool
	entityWidth, entityHeight, entityDepth int
	reusable                               [4]*pathNodeData
}

// horizontalDirections are Java's Direction.Plane.HORIZONTAL in order:
// north, east, south and west.
var horizontalDirections = [4]cube.Pos{{0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}}

// direction2D returns Direction.get2DDataValue of a horizontal direction.
func direction2D(i int) int { return [4]int{2, 3, 0, 1}[i] }

func (w *walkNodeEvaluator) prepare(m *Mob) {
	w.m = m
	w.nodes = make(map[int64]*pathNodeData, 128)
	w.types = make(map[int64]PathType, 256)
	w.collisions = make(map[cube.BBox]bool, 16)
	w.entityWidth = int(math.Floor(m.Width() + 1))
	w.entityHeight = int(math.Floor(m.Height() + 1))
	w.entityDepth = w.entityWidth
}

func (w *walkNodeEvaluator) done() {
	w.m, w.nodes, w.types, w.collisions = nil, nil, nil, nil
}

func (w *walkNodeEvaluator) node(x, y, z int) *pathNodeData {
	key := pathNodeKey(x, y, z)
	if n, ok := w.nodes[key]; ok {
		return n
	}
	n := newPathNode(x, y, z)
	w.nodes[key] = n
	return n
}

// blockAt returns the block at a position, or air if its chunk isn't
// loaded, as a PathNavigationRegion holds no unloaded chunks.
func (w *walkNodeEvaluator) blockAt(pos cube.Pos) world.Block {
	return pathBlockAt(w.m.tx, pos)
}

func pathBlockAt(tx *world.Tx, pos cube.Pos) world.Block {
	if pos.OutOfBounds(tx.Range()) {
		return block.Air{}
	}
	b, ok := tx.BlockLoaded(pos)
	if !ok {
		return block.Air{}
	}
	return b
}

// start ports WalkNodeEvaluator.getStart.
func (w *walkNodeEvaluator) start() *pathNodeData {
	m := w.m
	pos := m.data.Pos
	x, z := int(math.Floor(pos[0])), int(math.Floor(pos[2]))
	startY := int(math.Floor(pos[1]))
	switch {
	case w.canFloat && m.InWater():
		for pathWater(m.tx, cube.Pos{x, startY, z}) {
			startY++
		}
		startY--
	case m.OnGround():
		startY = int(math.Floor(pos[1] + .5))
	default:
		y := int(math.Floor(pos[1] + 1))
		for y > m.tx.Range().Min() {
			startY = y
			y--
			below := cube.Pos{x, y, z}
			if b := w.blockAt(below); !isAir(b) && !pathfindableLand(m.tx, below, b) {
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
	n.costMalus = float32(w.m.PathfindingMalus(n.typ))
	return n
}

func (w *walkNodeEvaluator) canStartAt(x, y, z int) bool {
	t := w.cachedPathType(x, y, z)
	return t != PathOpen && w.m.PathfindingMalus(t) >= 0
}

// neighbours ports WalkNodeEvaluator.getNeighbors.
func (w *walkNodeEvaluator) neighbours(out []*pathNodeData, pos *pathNodeData) int {
	p, jumpSize := 0, 0
	above := w.cachedPathType(pos.x, pos.y+1, pos.z)
	current := w.cachedPathType(pos.x, pos.y, pos.z)
	if w.m.PathfindingMalus(above) >= 0 && current != PathStickyHoney {
		jumpSize = int(math.Floor(math.Max(1, w.m.AI.StepHeight)))
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
		second := (i + 1) % 4
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
	return p
}

// diagonalValid ports WalkNodeEvaluator.isDiagonalValid(Node, Node, Node).
func (w *walkNodeEvaluator) diagonalValid(pos, ew, ns *pathNodeData) bool {
	if ns == nil || ew == nil || ns.y > pos.y || ew.y > pos.y {
		return false
	}
	if ew.typ == PathWalkableDoor || ns.typ == PathWalkableDoor {
		return false
	}
	if w.m.Width() > 1 && (ew.costMalus > 0 || ns.costMalus > 0) {
		return false
	}
	betweenPosts := ns.typ == PathFence && ew.typ == PathFence && w.m.Width() < .5
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

// floorLevel ports WalkNodeEvaluator.getFloorLevel.
func (w *walkNodeEvaluator) floorLevel(pos cube.Pos) float64 {
	if (w.canFloat || w.amphibious) && pathWater(w.m.tx, pos) {
		return float64(pos.Y()) + .5
	}
	return pathFloorLevel(w.m.tx, pos)
}

// pathFloorLevel returns the height of the floor of a position: the top of
// the collision shape of the block below.
func pathFloorLevel(tx *world.Tx, pos cube.Pos) float64 {
	below := pos.Side(cube.FaceDown)
	top, ok := collisionTop(tx, below, pathBlockAt(tx, below))
	if !ok {
		return float64(below.Y())
	}
	return float64(below.Y()) + top
}

// collisionTop returns the top of the collision shape of a block.
func collisionTop(tx *world.Tx, pos cube.Pos, b world.Block) (float64, bool) {
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
	if w.floorLevel(cube.Pos{x, y, z})-nodeHeight > math.Max(1.125, w.m.AI.StepHeight) {
		return nil
	}
	t := w.cachedPathType(x, y, z)
	cost := w.m.PathfindingMalus(t)
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
	width := w.m.Width()
	if width >= 1 || above.typ != PathOpen && above.typ != PathWalkable {
		return above
	}
	d := horizontalDirections[dir]
	cx, cz := float64(x-d[0])+.5, float64(z-d[2])+.5
	half := width / 2
	floorFrom := w.floorLevel(cube.PosFromVec3(mgl64.Vec3{cx, float64(y + 1), cz}))
	floorTo := w.floorLevel(cube.Pos{above.x, above.y, above.z})
	box := cube.Box(cx-half, floorFrom+.001, cz-half, cx+half, w.m.Height()+floorTo-.002, cz+half)
	if w.hasCollisions(box) {
		return nil
	}
	return above
}

func (w *walkNodeEvaluator) firstNonWaterBelow(x, y, z int, best *pathNodeData) *pathNodeData {
	for y--; y > w.m.tx.Range().Min(); y-- {
		t := w.cachedPathType(x, y, z)
		if t != PathWater {
			return best
		}
		best = w.nodeWithCost(x, y, z, t, w.m.PathfindingMalus(t))
	}
	return best
}

func (w *walkNodeEvaluator) firstGroundNodeBelow(x, y, z int) *pathNodeData {
	maxFall := w.m.maxFallDistance()
	for current := y - 1; current >= w.m.tx.Range().Min(); current-- {
		if y-current > maxFall {
			return w.blockedNode(x, current, z)
		}
		t := w.cachedPathType(x, current, z)
		cost := w.m.PathfindingMalus(t)
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
	if c, ok := w.collisions[box]; ok {
		return c
	}
	c := !boxFree(w.m.tx, box)
	w.collisions[box] = c
	return c
}

// boxFree reports if no block collides with a box.
func boxFree(tx *world.Tx, box cube.BBox) bool {
	minX, minY, minZ := int(math.Floor(box.Min()[0])), int(math.Floor(box.Min()[1]))-1, int(math.Floor(box.Min()[2]))
	maxX, maxY, maxZ := int(math.Floor(box.Max()[0])), int(math.Floor(box.Max()[1])), int(math.Floor(box.Max()[2]))
	for x := minX; x <= maxX; x++ {
		for y := minY; y <= maxY; y++ {
			for z := minZ; z <= maxZ; z++ {
				pos := cube.Pos{x, y, z}
				for _, bb := range pathBlockAt(tx, pos).Model().BBox(pos, tx) {
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
	if t, ok := w.types[key]; ok {
		return t
	}
	t := w.pathTypeOfMob(x, y, z)
	w.types[key] = t
	return t
}

// pathTypeOfMob ports WalkNodeEvaluator.getPathTypeOfMob: the type of the
// blocks the mob would stand in.
func (w *walkNodeEvaluator) pathTypeOfMob(x, y, z int) PathType {
	var types [pathTypeCount]bool
	count := 0
	var only PathType
	mobPos := w.m.BlockPos()
	for dx := range w.entityWidth {
		for dy := range w.entityHeight {
			for dz := range w.entityDepth {
				t := pathTypeStatic(w.m.tx, cube.Pos{x + dx, y + dy, z + dz})
				if t == PathDoorWoodClosed && w.canOpenDoors && w.canPassDoors {
					t = PathWalkableDoor
				}
				if t == PathDoorOpen && !w.canPassDoors {
					t = PathBlocked
				}
				if t == PathRail && pathTypeStatic(w.m.tx, mobPos) != PathRail && pathTypeStatic(w.m.tx, mobPos.Side(cube.FaceDown)) != PathRail {
					t = PathUnpassableRail
				}
				if !types[t] {
					types[t] = true
					count++
					only = t
				}
			}
		}
	}
	if count == 1 {
		return only
	}
	if types[PathFence] {
		return PathFence
	}
	if types[PathUnpassableRail] {
		return PathUnpassableRail
	}
	highest, highestMalus := PathBlocked, w.m.PathfindingMalus(PathBlocked)
	for t := range pathTypeCount {
		if !types[t] {
			continue
		}
		malus := w.m.PathfindingMalus(t)
		if malus < 0 {
			return t
		}
		if malus >= highestMalus {
			highest, highestMalus = t, malus
		}
	}
	current := pathTypeStatic(w.m.tx, cube.Pos{x, y, z})
	if w.entityWidth > 1 {
		cheaper := w.m.PathfindingMalus(current) < highestMalus
		if cheaper && w.m.PathfindingMalus(PathBigMobsCloseToDanger) < highestMalus {
			return PathBigMobsCloseToDanger
		}
		return highest
	}
	if current == PathOpen && highest != PathOpen && highestMalus == 0 {
		return PathOpen
	}
	return highest
}

// pathType returns the path type of a single block for the mob, as
// NodeEvaluator.getPathType.
func (w *walkNodeEvaluator) pathType(m *Mob, pos cube.Pos) PathType {
	return pathTypeStatic(m.tx, pos)
}

// pathTypeStatic ports WalkNodeEvaluator.getPathTypeStatic: the type of a
// block, where an open block above a floor is walkable unless the floor or
// a neighbour is dangerous.
func pathTypeStatic(tx *world.Tx, pos cube.Pos) PathType {
	t := pathTypeFromState(tx, pos)
	if t != PathOpen || pos.Y() < tx.Range().Min()+1 {
		return t
	}
	switch below := pathTypeFromState(tx, pos.Side(cube.FaceDown)); below {
	case PathOpen, PathWater, PathLava, PathWalkable:
		return PathOpen
	case PathFire, PathDamaging, PathStickyHoney, PathDamageCautious:
		return below
	case PathPowderSnow:
		return PathOnTopOfPowderSnow
	case PathTrapdoor:
		return PathOnTopOfTrapdoor
	}
	return checkNeighbourBlocks(tx, pos, PathWalkable)
}

// checkNeighbourBlocks ports WalkNodeEvaluator.checkNeighbourBlocks.
func checkNeighbourBlocks(tx *world.Tx, pos cube.Pos, t PathType) PathType {
	for dx := -1; dx <= 1; dx++ {
		for dy := -1; dy <= 1; dy++ {
			for dz := -1; dz <= 1; dz++ {
				if dx == 0 && dz == 0 {
					continue
				}
				switch pathTypeFromState(tx, pos.Add(cube.Pos{dx, dy, dz})) {
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

// pathTypesByState caches the path types of blocks by Java block state ID,
// as the PathTypeCache of a ServerLevel caches them by position.
var pathTypesByState sync.Map

// pathTypeFromState ports WalkNodeEvaluator.getPathTypeFromState.
func pathTypeFromState(tx *world.Tx, pos cube.Pos) PathType {
	if pos.OutOfBounds(tx.Range()) {
		return PathOpen
	}
	b, ok := tx.BlockLoaded(pos)
	if !ok {
		return PathOpen
	}
	if _, air := b.(block.Air); air {
		if _, wet := tx.Liquid(pos); !wet {
			return PathOpen
		}
	}
	id, err := tx.BlockStateID(pos)
	if err == nil && id != 0 {
		if t, ok := pathTypesByState.Load(id); ok {
			return t.(PathType)
		}
	}
	t := computePathType(tx, pos, b, id)
	if err == nil && id != 0 {
		pathTypesByState.Store(id, t)
	}
	return t
}

func computePathType(tx *world.Tx, pos cube.Pos, b world.Block, id uint32) PathType {
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
func pathfindableLand(tx *world.Tx, pos cube.Pos, b world.Block) bool {
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
func pathfindableLandState(tx *world.Tx, pos cube.Pos, b world.Block, name string, props map[string]any, tagged func(string) bool) bool {
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
func fullCollisionCube(tx *world.Tx, pos cube.Pos, b world.Block) bool {
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
func pathWater(tx *world.Tx, pos cube.Pos) bool {
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
