package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// transportState is TransportItemsBetweenContainers.TransportItemState: what
// a mob carrying items between containers does.
type transportState uint8

const (
	transportTravelling transportState = iota
	transportQueuing
	transportInteracting
)

// transportInteraction is
// TransportItemsBetweenContainers.ContainerInteractionState: what a mob does
// at the container it reached.
type transportInteraction uint8

const (
	transportPickupItem transportInteraction = iota
	transportPickupNoItem
	transportPlaceItem
	transportPlaceNoItem
)

// transportChest is the block entity of a chest, which identifies it by the
// inventory of its own half.
type transportChest interface {
	world.Block
	OwnInventory() *inventory.Inventory
	PairedWith(pos cube.Pos) (cube.Pos, bool)
	Inventory(tx *world.Tx, pos cube.Pos) *inventory.Inventory
	Opened() bool
}

// transportTarget is TransportItemsBetweenContainers.TransportItemTarget: a
// chest and the block it was when the mob picked it.
type transportTarget struct {
	pos   cube.Pos
	chest transportChest
}

// inventory returns the container of the target, both halves of a double
// chest.
func (t transportTarget) inventory(tx *world.Tx) *inventory.Inventory {
	return t.chest.Inventory(tx, t.pos)
}

// transportItems ports TransportItemsBetweenContainers: a mob with an empty
// hand takes items out of the nearest source container, and carries them to
// the nearest destination container that is empty or holds the same item.
type transportItems struct {
	speed                float64
	source, destination  func(world.Block) bool
	horizontal, vertical int
	// onReached is called every tick the mob interacts with the container
	// it reached, with the ticks since it got there.
	onReached func(m *Mob, t transportTarget, interaction transportInteraction, ticks int)
	// onSetOff is called whenever the mob sets off.
	onSetOff func(m *Mob)
	// shouldQueue reports if another mob uses a container the mob should
	// wait for.
	shouldQueue func(m *Mob, t transportTarget) bool

	target         *transportTarget
	state          transportState
	interaction    transportInteraction
	hasInteraction bool
	ticksReached   int
}

// newTransportItemsBetweenContainers returns the TransportItemsBetweenContainers
// behaviour.
func newTransportItemsBetweenContainers(t *transportItems) *behavior {
	bh := &behavior{
		entry: []memoryCondition{
			{memVisitedBlockPositions, memRegistered}, {memUnreachableTransportBlockPositions, memRegistered},
			{memTransportItemsCooldownTicks, memAbsent}, {memIsPanicking, memAbsent},
		},
		noTimeout: true,
	}
	bh.start = func(m *Mob, _ int64) { m.AI.nav.CanPathToTargetsBelowSurface = true }
	bh.canStillUse = func(m *Mob, _ int64) bool {
		b := m.Brain()
		return !b.has(memTransportItemsCooldownTicks) && !b.has(memIsPanicking)
	}
	bh.stop = func(m *Mob, _ int64) { t.stop(m) }
	bh.tick = func(m *Mob, _ int64) { t.tick(m) }
	return bh
}

// stop ports TransportItemsBetweenContainers.stop.
func (t *transportItems) stop(m *Mob) {
	t.startTravelling(m)
	m.AI.nav.CanPathToTargetsBelowSurface = false
}

func (t *transportItems) tick(m *Mob) {
	updated := t.updateInvalidTarget(m)
	if t.target == nil {
		// Java calls stop without stopping the behaviour, which the
		// cooldown then stops.
		t.stop(m)
		return
	}
	if updated {
		return
	}
	if t.state == transportQueuing {
		t.onQueuing(m)
	}
	if t.state == transportTravelling {
		t.onTravelling(m)
	}
	if t.state == transportInteracting {
		t.onReachedTarget(m)
	}
}

// updateInvalidTarget ports TransportItemsBetweenContainers.updateInvalidTarget:
// a mob without a valid target looks for the nearest container it wants.
func (t *transportItems) updateInvalidTarget(m *Mob) bool {
	if t.hasValidTarget(m) {
		return false
	}
	t.stopTargeting(m)
	if target, ok := t.findTarget(m); ok {
		t.target = &target
		t.startTravelling(m)
		t.setVisited(m, target.pos)
		return true
	}
	t.enterCooldown(m)
	return true
}

func (t *transportItems) onQueuing(m *Mob) {
	if !t.anotherMobInteracting(m, *t.target) {
		t.state = transportTravelling
		t.walkTowardsTarget(m)
	}
}

func (t *transportItems) onTravelling(m *Mob) {
	target := *t.target
	switch {
	case t.withinDistance(m, 3, target, transportCentre(m, m.data.Pos)) && t.anotherMobInteracting(m, target):
		transportStopInPlace(m)
		t.state = transportQueuing
	case t.withinDistance(m, transportInteractionRange(m), target, transportCentre(m, m.data.Pos)):
		t.interaction, t.hasInteraction = t.reachedInteraction(m, target.inventory(m.tx)), true
		t.state = transportInteracting
	default:
		t.walkTowardsTarget(m)
	}
}

// onReachedTarget ports TransportItemsBetweenContainers.onReachedTarget: the
// mob stays at the container for three seconds, then takes or leaves items.
func (t *transportItems) onReachedTarget(m *Mob) {
	target := *t.target
	if !t.withinDistance(m, 2, target, transportCentre(m, m.data.Pos)) {
		t.startTravelling(m)
		return
	}
	t.ticksReached++
	m.Brain().set(memLookTarget, newBlockPosTracker(target.pos))
	transportStopInPlace(m)
	if t.hasInteraction && t.onReached != nil {
		t.onReached(m, target, t.interaction, t.ticksReached)
	}
	if t.ticksReached < 60 {
		return
	}
	inv := target.inventory(m.tx)
	switch t.reachedInteraction(m, inv) {
	case transportPickupItem:
		t.pickUpItems(m, inv)
	case transportPlaceItem:
		t.putDownItem(m, inv)
	default:
		t.stopTargeting(m)
	}
	t.startTravelling(m)
}

// reachedInteraction ports TransportItemsBetweenContainers.doReachedTargetInteraction:
// a mob with an empty hand takes items from a container with items in it,
// and one carrying items leaves them in a container that is empty or holds
// the same item.
func (t *transportItems) reachedInteraction(m *Mob, inv *inventory.Inventory) transportInteraction {
	if transportHeld(m).Empty() {
		if !inventoryEmpty(inv) {
			return transportPickupItem
		}
		return transportPickupNoItem
	}
	if inventoryEmpty(inv) || inventoryHasSameItem(inv, transportHeld(m)) {
		return transportPlaceItem
	}
	return transportPlaceNoItem
}

func (t *transportItems) walkTowardsTarget(m *Mob) {
	if t.target != nil {
		setWalkAndLookTarget(m, newBlockPosTracker(t.target.pos), t.speed, 0)
	}
}

func (t *transportItems) startTravelling(m *Mob) {
	if t.onSetOff != nil {
		t.onSetOff(m)
	}
	t.state = transportTravelling
	t.hasInteraction = false
	t.ticksReached = 0
}

// findTarget ports TransportItemsBetweenContainers.getTransportTarget: the
// nearest chest in the loaded chunks around the mob that it wants and did
// not visit yet.
func (t *transportItems) findTarget(m *Mob) (transportTarget, bool) {
	h, v := t.horizontal, t.vertical
	if m.data.Riding != nil {
		h, v = 1, 1
	}
	bp := m.BlockPos()
	lo := cube.Pos{bp[0] - h, bp[1] - v, bp[2] - h}
	hi := cube.Pos{bp[0] + 1 + h, bp[1] + 1 + v, bp[2] + 1 + h}
	visited, unreachable := transportPositions(m, memVisitedBlockPositions), transportPositions(m, memUnreachableTransportBlockPositions)
	r := int(math.Floor(float64(h)/16)) + 1
	cx, cz := bp[0]>>4, bp[2]>>4
	var best transportTarget
	found, closest := false, float64(math.MaxFloat32)
	for x := cx - r; x <= cx+r; x++ {
		for z := cz - r; z <= cz+r; z++ {
			for pos, b := range m.tx.LoadedBlockEntities(world.ChunkPos{int32(x), int32(z)}) {
				chest, ok := b.(transportChest)
				if !ok {
					continue
				}
				d := pos.Vec3Centre().Sub(m.data.Pos).LenSqr()
				if d >= closest {
					continue
				}
				if pos[0] < lo[0] || pos[0] >= hi[0] || pos[1] < lo[1] || pos[1] >= hi[1] || pos[2] < lo[2] || pos[2] >= hi[2] {
					continue
				}
				target := transportTarget{pos: pos, chest: chest}
				if !t.wanted(m, chest) || t.alreadyVisited(m, target, visited, unreachable) {
					continue
				}
				best, found, closest = target, true, d
			}
		}
	}
	return best, found
}

// connectedTargets ports TransportItemsBetweenContainers.getConnectedTargets:
// both halves of a double chest.
func (t *transportItems) connectedTargets(m *Mob, target transportTarget) []transportTarget {
	targets := []transportTarget{target}
	if pair, ok := target.chest.PairedWith(target.pos); ok {
		if chest, ok := m.tx.Block(pair).(transportChest); ok {
			targets = append(targets, transportTarget{pos: pair, chest: chest})
		}
	}
	return targets
}

func (t *transportItems) alreadyVisited(m *Mob, target transportTarget, visited, unreachable map[globalPos]struct{}) bool {
	dim := m.tx.World().Dimension()
	for _, c := range t.connectedTargets(m, target) {
		g := globalPos{dim: dim, pos: c.pos}
		_, v := visited[g]
		_, u := unreachable[g]
		if v || u {
			return true
		}
	}
	return false
}

// hasValidTarget ports TransportItemsBetweenContainers.hasValidTarget: the
// target is still the chest the mob wants, nothing blocks its lid, and a
// travelling mob still has a way to reach it.
func (t *transportItems) hasValidTarget(m *Mob) bool {
	if t.target == nil {
		return false
	}
	target := *t.target
	current, ok := m.tx.Block(target.pos).(transportChest)
	if !t.wanted(m, target.chest) || !ok || current.OwnInventory() != target.chest.OwnInventory() || chestBlockedAt(m.tx, target.pos) {
		return false
	}
	if t.state != transportTravelling || t.hasValidTravellingPath(m, target) {
		return true
	}
	t.markUnreachable(m, target.pos)
	return false
}

// hasValidTravellingPath ports
// TransportItemsBetweenContainers.hasValidTravellingPath.
func (t *transportItems) hasValidTravellingPath(m *Mob, target transportTarget) bool {
	path := m.AI.nav.Path()
	if path == nil {
		path = m.AI.nav.CreatePath(m, target.pos, 0)
	}
	from := m.data.Pos
	if path != nil && path.EndNode() != nil {
		from = path.EndNode().Pos().Vec3Middle()
		from[1] = float64(path.EndNode().Pos()[1])
	}
	from = transportCentre(m, from)
	canReach := t.withinDistance(m, transportInteractionRange(m), target, from)
	if path == nil && !canReach {
		return true
	}
	return canReach && transportSeesAnySide(m, target.pos, from)
}

// transportCentre ports TransportItemsBetweenContainers.setMiddleYPosition.
func transportCentre(m *Mob, pos mgl64.Vec3) mgl64.Vec3 {
	return pos.Add(mgl64.Vec3{0, m.Height() / 2})
}

// transportInteractionRange ports TransportItemsBetweenContainers.getInteractionRange:
// a mob at the end of its path reaches a block further.
func transportInteractionRange(m *Mob) float64 {
	if p := m.AI.nav.Path(); p != nil && p.Done() {
		return 1
	}
	return .5
}

// withinDistance ports TransportItemsBetweenContainers.isWithinTargetDistance:
// the box of the mob at from touches the collision box of the chest grown by
// distance sideways and half a block up and down.
func (t *transportItems) withinDistance(m *Mob, distance float64, target transportTarget, from mgl64.Vec3) bool {
	boxes := target.chest.Model().BBox(target.pos, m.tx)
	if len(boxes) == 0 {
		return false
	}
	bounds := boxes[0]
	for _, bb := range boxes[1:] {
		bounds = cube.Box(math.Min(bounds.Min()[0], bb.Min()[0]), math.Min(bounds.Min()[1], bb.Min()[1]), math.Min(bounds.Min()[2], bb.Min()[2]),
			math.Max(bounds.Max()[0], bb.Max()[0]), math.Max(bounds.Max()[1], bb.Max()[1]), math.Max(bounds.Max()[2], bb.Max()[2]))
	}
	reach := bounds.GrowVec3(mgl64.Vec3{distance, .5, distance}).Translate(target.pos.Vec3())
	w, h := m.Width(), m.Height()
	box := cube.Box(from[0]-w/2, from[1]-h/2, from[2]-w/2, from[0]+w/2, from[1]+h/2, from[2]+w/2)
	return reach.IntersectsWith(box)
}

// transportSeesAnySide ports TransportItemsBetweenContainers.canSeeAnyTargetSide:
// a line from the eyes of the mob to the middle of a side of the chest hits
// the chest first.
func transportSeesAnySide(m *Mob, pos cube.Pos, eye mgl64.Vec3) bool {
	centre := pos.Vec3Centre()
	for _, face := range cube.Faces() {
		side := cube.Pos{}.Side(face).Vec3().Mul(.5)
		if clipHitsBlock(m.tx, eye, centre.Add(side), pos) {
			return true
		}
	}
	return false
}

// clipHitsBlock reports if a clip of collision boxes from one point to
// another first hits the block at pos.
func clipHitsBlock(tx *world.Tx, from, to mgl64.Vec3, pos cube.Pos) bool {
	hits := false
	trace.TraverseBlocks(from, to, func(at cube.Pos) bool {
		if _, hit := trace.BlockIntercept(at, tx, pathBlockAt(tx, at), from, to, trace.CollisionShape); hit {
			hits = at == pos
			return false
		}
		return true
	})
	return hits
}

// chestBlockedAt ports ChestBlock.isChestBlockedAt: a redstone conductor on
// top of the chest, or a cat sitting on it, keeps its lid shut.
func chestBlockedAt(tx *world.Tx, pos cube.Pos) bool {
	if redstoneConductor(tx.Block(pos.Side(cube.FaceUp))) {
		return true
	}
	box := cube.Box(0, 1, 0, 1, 2, 1).Translate(pos.Vec3())
	for e := range tx.EntitiesWithin(box) {
		if c, ok := e.(*Cat); ok && c.Sitting() && e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
			return true
		}
	}
	return false
}

// redstoneConductor reports if a block conducts redstone, as
// BlockStateBase.isRedstoneConductor.
func redstoneConductor(b world.Block) bool {
	state, err := world.CanonicalStateOf(b)
	if err != nil {
		return false
	}
	id, err := gamedata.BlockStateID(world.NormaliseBlockName(state.Name), state.Properties)
	if err != nil {
		return false
	}
	conductor, _ := gamedata.BlockRedstoneConductorID(id)
	return conductor
}

func (t *transportItems) anotherMobInteracting(m *Mob, target transportTarget) bool {
	for _, c := range t.connectedTargets(m, target) {
		if t.shouldQueue(m, c) {
			return true
		}
	}
	return false
}

// wanted ports TransportItemsBetweenContainers.isWantedBlock: a mob with an
// empty hand wants a source, and one carrying items a destination.
func (t *transportItems) wanted(m *Mob, b world.Block) bool {
	if transportHeld(m).Empty() {
		return t.source(b)
	}
	return t.destination(b)
}

// transportPositions returns the positions a memory holds.
func transportPositions(m *Mob, mem memoryType) map[globalPos]struct{} {
	v, _ := m.Brain().get(mem)
	set, _ := v.(map[globalPos]struct{})
	return set
}

// setVisited ports TransportItemsBetweenContainers.setVisitedBlockPos: a mob
// that visited more than ten containers gives up for a while.
func (t *transportItems) setVisited(m *Mob, pos cube.Pos) {
	visited := copyPositions(transportPositions(m, memVisitedBlockPositions))
	visited[globalPos{dim: m.tx.World().Dimension(), pos: pos}] = struct{}{}
	if len(visited) > 10 {
		t.enterCooldown(m)
		return
	}
	m.Brain().setWithExpiry(memVisitedBlockPositions, visited, 6000)
}

// markUnreachable ports
// TransportItemsBetweenContainers.markVisitedBlockPosAsUnreachable.
func (t *transportItems) markUnreachable(m *Mob, pos cube.Pos) {
	g := globalPos{dim: m.tx.World().Dimension(), pos: pos}
	visited := copyPositions(transportPositions(m, memVisitedBlockPositions))
	delete(visited, g)
	unreachable := copyPositions(transportPositions(m, memUnreachableTransportBlockPositions))
	unreachable[g] = struct{}{}
	if len(unreachable) > 50 {
		t.enterCooldown(m)
		return
	}
	b := m.Brain()
	b.setWithExpiry(memVisitedBlockPositions, visited, 6000)
	b.setWithExpiry(memUnreachableTransportBlockPositions, unreachable, 6000)
}

func copyPositions(set map[globalPos]struct{}) map[globalPos]struct{} {
	c := make(map[globalPos]struct{}, len(set)+1)
	for k := range set {
		c[k] = struct{}{}
	}
	return c
}

// pickUpItems ports TransportItemsBetweenContainers.pickUpItems: the mob
// takes up to sixteen items of the first stack of the container.
func (t *transportItems) pickUpItems(m *Mob, inv *inventory.Inventory) {
	for slot, s := range inv.Slots() {
		if s.Empty() {
			continue
		}
		n := min(s.Count(), 16)
		_ = inv.SetItem(slot, s.Grow(-n))
		setTransportHeld(m, s.Grow(n-s.Count()))
		break
	}
	t.clearAfterMatch(m)
}

// putDownItem ports TransportItemsBetweenContainers.putDownItem and
// addItemsToContainer: the items go into the first empty slot, topping up
// the stacks of the same item before it.
func (t *transportItems) putDownItem(m *Mob, inv *inventory.Inventory) {
	held := transportHeld(m)
	left := held
	for slot, s := range inv.Slots() {
		if s.Empty() {
			_ = inv.SetItem(slot, left)
			left = item.Stack{}
			break
		}
		if s.Comparable(left) && s.Count() < s.MaxCount() {
			room := s.MaxCount() - s.Count()
			_ = inv.SetItem(slot, s.Grow(min(room, left.Count())))
			// Java takes away the room left in the stack rather than
			// what it put in it.
			if left.Count() <= room {
				left = item.Stack{}
				break
			}
			left = left.Grow(-room)
		}
	}
	setTransportHeld(m, left)
	if left.Empty() {
		t.clearAfterMatch(m)
	} else {
		t.stopTargeting(m)
	}
}

// stopTargeting ports TransportItemsBetweenContainers.stopTargetingCurrentTarget.
func (t *transportItems) stopTargeting(m *Mob) {
	t.ticksReached = 0
	t.target = nil
	m.AI.nav.Stop()
	m.Brain().erase(memWalkTarget)
}

// clearAfterMatch ports
// TransportItemsBetweenContainers.clearMemoriesAfterMatchingTargetFound.
func (t *transportItems) clearAfterMatch(m *Mob) {
	t.stopTargeting(m)
	b := m.Brain()
	b.erase(memVisitedBlockPositions)
	b.erase(memUnreachableTransportBlockPositions)
}

// enterCooldown ports
// TransportItemsBetweenContainers.enterCooldownAfterNoMatchingTargetFound.
func (t *transportItems) enterCooldown(m *Mob) {
	t.stopTargeting(m)
	b := m.Brain()
	b.set(memTransportItemsCooldownTicks, 140)
	b.erase(memVisitedBlockPositions)
	b.erase(memUnreachableTransportBlockPositions)
}

// transportStopInPlace ports TransportItemsBetweenContainers.stopInPlace: the
// mob stops, but keeps falling.
func transportStopInPlace(m *Mob) {
	m.AI.nav.Stop()
	m.AI.xxa, m.AI.yya = 0, 0
	m.SetSpeed(0)
	m.data.Vel[0], m.data.Vel[2] = 0, 0
}

// transportCarrier is a mob that carries items between containers in its
// main hand.
type transportCarrier interface {
	transportHeld() item.Stack
	setTransportHeld(s item.Stack)
}

func transportHeld(m *Mob) item.Stack {
	if c, ok := m.Behaviour().(transportCarrier); ok {
		return c.transportHeld()
	}
	return item.Stack{}
}

func setTransportHeld(m *Mob, s item.Stack) {
	if c, ok := m.Behaviour().(transportCarrier); ok {
		c.setTransportHeld(s)
	}
}

// inventoryEmpty reports if no slot of an inventory holds items, as
// Container.isEmpty.
func inventoryEmpty(inv *inventory.Inventory) bool {
	for _, s := range inv.Slots() {
		if !s.Empty() {
			return false
		}
	}
	return true
}

// inventoryHasSameItem ports TransportItemsBetweenContainers.hasItemMatchingHandItem:
// a stack of the same item, whatever its components.
func inventoryHasSameItem(inv *inventory.Inventory, held item.Stack) bool {
	name, _ := held.Item().EncodeItem()
	for _, s := range inv.Slots() {
		if s.Empty() {
			continue
		}
		if n, _ := s.Item().EncodeItem(); n == name {
			return true
		}
	}
	return false
}

var _ transportChest = block.Chest{}
