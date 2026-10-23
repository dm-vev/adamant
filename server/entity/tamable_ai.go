package entity

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports Java's TamableAnimal and the goals tamed animals share.

// tamable is the state of Java's TamableAnimal: whether the animal is tamed
// and by whom, and whether its owner ordered it to sit and it sits.
type tamable struct {
	tamed     bool
	owner     *world.EntityHandle
	ownerUUID string
	// orderedToSit is TamableAnimal.orderedToSit, inSittingPose the sitting
	// flag SitWhenOrderedToGoal sets while the animal sits.
	orderedToSit, inSittingPose bool
}

// tamableBehaviour is implemented by behaviours of tamed animals.
type tamableBehaviour interface {
	tamableState() *tamable
	// wantsToAttack ports TamableAnimal.wantsToAttack.
	wantsToAttack(m *Mob, target, owner world.Entity) bool
}

func tamableOf(m *Mob) *tamable { return m.Behaviour().(tamableBehaviour).tamableState() }

// ownerEntity returns the owner of the animal if it is in the world.
func (t *tamable) ownerEntity(tx *world.Tx) world.Entity {
	if t.owner == nil && t.ownerUUID != "" {
		for e := range tx.Players() {
			if e.H().UUID().String() == t.ownerUUID {
				t.owner = e.H()
				break
			}
		}
	}
	if t.owner == nil {
		return nil
	}
	e, ok := t.owner.Entity(tx)
	if !ok {
		return nil
	}
	return e
}

// ownedBy ports TamableAnimal.isOwnedBy.
func (t *tamable) ownedBy(e world.Entity) bool {
	return t.tamed && e != nil && t.ownerUUID != "" && e.H().UUID().String() == t.ownerUUID
}

// unableToMoveToOwner ports TamableAnimal.unableToMoveToOwner.
func (t *tamable) unableToMoveToOwner(m *Mob) bool {
	if t.orderedToSit || m.data.Riding != nil || m.Ent.HolderLeashID() != 0 {
		return true
	}
	owner := t.ownerEntity(m.tx)
	return owner != nil && spectator(owner)
}

// shouldTryTeleportToOwner ports TamableAnimal.shouldTryTeleportToOwner.
func (t *tamable) shouldTryTeleportToOwner(m *Mob) bool {
	owner := t.ownerEntity(m.tx)
	return owner != nil && m.DistanceToSqr(owner) >= 144
}

// tryToTeleportToOwner ports TamableAnimal.tryToTeleportToOwner: the animal
// moves to one of the walkable spots two or three blocks around its owner.
func (t *tamable) tryToTeleportToOwner(m *Mob, canFly bool) {
	owner := t.ownerEntity(m.tx)
	if owner == nil {
		return
	}
	target := cube.PosFromVec3(owner.Position())
	r := m.Random()
	for range 10 {
		xd, zd := int(r.NextInt(7))-3, int(r.NextInt(7))-3
		if max(xd, -xd) < 2 && max(zd, -zd) < 2 {
			continue
		}
		yd := int(r.NextInt(3)) - 1
		pos := target.Add(cube.Pos{xd, yd, zd})
		if !canTeleportTo(m, pos, canFly) {
			continue
		}
		m.Teleport(mgl64.Vec3{float64(pos[0]) + .5, float64(pos[1]), float64(pos[2]) + .5})
		m.AI.nav.Stop()
		return
	}
}

// canTeleportTo ports TamableAnimal.canTeleportTo: a walkable spot, not on
// leaves for an animal that cannot fly, where the animal fits.
func canTeleportTo(m *Mob, pos cube.Pos, canFly bool) bool {
	if pathTypeStatic(m.tx, pos) != PathWalkable {
		return false
	}
	if _, leaves := m.tx.Block(pos.Side(cube.FaceDown)).(block.Leaves); leaves && !canFly {
		return false
	}
	delta := pos.Vec3().Sub(cube.PosFromVec3(m.data.Pos).Vec3())
	return !boxCollidesWithBlocks(m.tx, m.BBox().Translate(delta))
}

// boxCollidesWithBlocks reports if a box overlaps the collision box of a
// block.
func boxCollidesWithBlocks(tx *world.Tx, box cube.BBox) bool {
	boxes := blockBBoxsAround(tx, box)
	defer func() {
		if boxes != nil {
			blockBBoxPool.Put(boxes[:0])
		}
	}()
	for _, b := range boxes {
		if b.IntersectsWith(box) {
			return true
		}
	}
	return false
}

// tamableAnimalPanicGoal ports TamableAnimal.TamableAnimalPanicGoal: a
// panicking pet far from its owner teleports to it.
type tamableAnimalPanicGoal struct{ *PanicGoal }

func (g tamableAnimalPanicGoal) Tick(m *Mob) {
	t := tamableOf(m)
	if !t.unableToMoveToOwner(m) && t.shouldTryTeleportToOwner(m) {
		t.tryToTeleportToOwner(m, false)
	}
}

// sitWhenOrderedToGoal ports SitWhenOrderedToGoal: a pet its owner ordered
// to sit sits down once on the ground and out of water, unless its owner
// nearby was just hurt.
type sitWhenOrderedToGoal struct{ goalFlags }

func newSitWhenOrderedToGoal() *sitWhenOrderedToGoal {
	return &sitWhenOrderedToGoal{goalFlags{GoalJump | GoalMove}}
}

func (g *sitWhenOrderedToGoal) CanContinueToUse(m *Mob) bool { return tamableOf(m).orderedToSit }

func (g *sitWhenOrderedToGoal) CanUse(m *Mob) bool {
	t := tamableOf(m)
	if !t.orderedToSit && !t.tamed || m.InWater() || !m.OnGround() {
		return false
	}
	owner := t.ownerEntity(m.tx)
	if owner == nil {
		return true
	}
	if m.DistanceToSqr(owner) < 144 {
		if attacker, _ := ownerLastHurtBy(m.tx, owner); attacker != nil {
			return false
		}
	}
	return t.orderedToSit
}

func (g *sitWhenOrderedToGoal) Start(m *Mob) {
	m.AI.nav.Stop()
	tamableOf(m).inSittingPose = true
}

func (g *sitWhenOrderedToGoal) Stop(m *Mob) { tamableOf(m).inSittingPose = false }

// followOwnerGoal ports FollowOwnerGoal: a pet more than startDistance away
// from its owner walks back to within stopDistance, wading through water,
// and teleports to its owner from 12 blocks away.
type followOwnerGoal struct {
	goalFlags
	speed, startDistance, stopDistance float64
	// canFly lets the pet teleport onto leaves, as parrots may.
	canFly bool

	owner            *world.EntityHandle
	timeToRecalcPath int
	oldWaterCost     float64
}

func newFollowOwnerGoal(speed, startDistance, stopDistance float64) *followOwnerGoal {
	return &followOwnerGoal{goalFlags: goalFlags{GoalMove | GoalLook}, speed: speed, startDistance: startDistance, stopDistance: stopDistance}
}

func (g *followOwnerGoal) CanUse(m *Mob) bool {
	t := tamableOf(m)
	owner := t.ownerEntity(m.tx)
	if owner == nil || t.unableToMoveToOwner(m) || m.DistanceToSqr(owner) < g.startDistance*g.startDistance {
		return false
	}
	g.owner = owner.H()
	return true
}

func (g *followOwnerGoal) CanContinueToUse(m *Mob) bool {
	if m.AI.nav.IsDone() || tamableOf(m).unableToMoveToOwner(m) {
		return false
	}
	owner, ok := g.owner.Entity(m.tx)
	return ok && m.DistanceToSqr(owner) > g.stopDistance*g.stopDistance
}

func (g *followOwnerGoal) Start(m *Mob) {
	g.timeToRecalcPath = 0
	g.oldWaterCost = m.PathfindingMalus(PathWater)
	m.AI.SetPathfindingMalus(PathWater, 0)
}

func (g *followOwnerGoal) Stop(m *Mob) {
	g.owner = nil
	m.AI.nav.Stop()
	m.AI.SetPathfindingMalus(PathWater, g.oldWaterCost)
}

func (g *followOwnerGoal) Tick(m *Mob) {
	t := tamableOf(m)
	owner, ok := g.owner.Entity(m.tx)
	if !ok {
		return
	}
	far := t.shouldTryTeleportToOwner(m)
	if !far {
		m.AI.look.SetLookAtEntityWith(owner, 10, m.AI.MaxHeadPitch)
	}
	if g.timeToRecalcPath--; g.timeToRecalcPath > 0 {
		return
	}
	g.timeToRecalcPath = adjustedTickDelay(g, 10)
	if far {
		t.tryToTeleportToOwner(m, g.canFly)
	} else {
		m.AI.nav.MoveToEntity(m, owner, g.speed)
	}
}

// ownerLastHurtBy returns the living entity that hurt the owner of a pet in
// the last 100 ticks and when.
func ownerLastHurtBy(tx *world.Tx, owner world.Entity) (world.Entity, int64) {
	if o, ok := owner.(interface{ LastHurtByMob() (world.Entity, int64) }); ok {
		return o.LastHurtByMob()
	}
	if m, ok := mobOf(owner); ok {
		e, t := m.LastHurtBy()
		return e, int64(t)
	}
	return nil, 0
}

// ownerHurtByTargetGoal ports OwnerHurtByTargetGoal: a pet targets whatever
// hurt its owner.
type ownerHurtByTargetGoal struct {
	targetGoal
	attacker  *world.EntityHandle
	timestamp int64
}

func newOwnerHurtByTargetGoal() *ownerHurtByTargetGoal {
	return &ownerHurtByTargetGoal{targetGoal: newTargetGoal(false, false), timestamp: -1}
}

func (g *ownerHurtByTargetGoal) CanUse(m *Mob) bool {
	t := tamableOf(m)
	if !t.tamed || t.orderedToSit {
		return false
	}
	owner := t.ownerEntity(m.tx)
	if owner == nil {
		return false
	}
	src, ok := owner.(interface {
		LastDamageSource(timeout int64) world.DamageSource
	})
	if !ok || src.LastDamageSource(100) == nil {
		return false
	}
	attacker, ts := ownerLastHurtBy(m.tx, owner)
	if ts == g.timestamp || !g.canAttack(m, attacker, forCombat()) || !m.Behaviour().(tamableBehaviour).wantsToAttack(m, attacker, owner) {
		return false
	}
	g.attacker = attacker.H()
	return true
}

func (g *ownerHurtByTargetGoal) Start(m *Mob) {
	if attacker, ok := g.attacker.Entity(m.tx); ok {
		m.SetTarget(attacker)
	}
	if owner := tamableOf(m).ownerEntity(m.tx); owner != nil {
		_, g.timestamp = ownerLastHurtBy(m.tx, owner)
	}
	g.start()
}

func (g *ownerHurtByTargetGoal) CanContinueToUse(m *Mob) bool { return g.canContinue(m) }

func (g *ownerHurtByTargetGoal) Stop(m *Mob) { g.stop(m) }

// ownerHurtTargetGoal ports OwnerHurtTargetGoal: a pet targets whatever its
// owner hurt.
type ownerHurtTargetGoal struct {
	targetGoal
	victim    *world.EntityHandle
	timestamp int64
}

func newOwnerHurtTargetGoal() *ownerHurtTargetGoal {
	return &ownerHurtTargetGoal{targetGoal: newTargetGoal(false, false), timestamp: -1}
}

func ownerLastHurtMob(owner world.Entity) (world.Entity, int64) {
	if o, ok := owner.(interface{ LastHurtMob() (world.Entity, int64) }); ok {
		return o.LastHurtMob()
	}
	if m, ok := mobOf(owner); ok {
		e, t := m.LastHurtMob()
		return e, int64(t)
	}
	return nil, 0
}

func (g *ownerHurtTargetGoal) CanUse(m *Mob) bool {
	t := tamableOf(m)
	if !t.tamed || t.orderedToSit {
		return false
	}
	owner := t.ownerEntity(m.tx)
	if owner == nil {
		return false
	}
	victim, ts := ownerLastHurtMob(owner)
	if ts == g.timestamp || !g.canAttack(m, victim, forCombat()) || !m.Behaviour().(tamableBehaviour).wantsToAttack(m, victim, owner) {
		return false
	}
	g.victim = victim.H()
	return true
}

func (g *ownerHurtTargetGoal) Start(m *Mob) {
	if victim, ok := g.victim.Entity(m.tx); ok {
		m.SetTarget(victim)
	}
	if owner := tamableOf(m).ownerEntity(m.tx); owner != nil {
		_, g.timestamp = ownerLastHurtMob(owner)
	}
	g.start()
}

func (g *ownerHurtTargetGoal) CanContinueToUse(m *Mob) bool { return g.canContinue(m) }

func (g *ownerHurtTargetGoal) Stop(m *Mob) { g.stop(m) }

// nonTameRandomTargetGoal ports NonTameRandomTargetGoal: an untamed animal
// now and then hunts the nearest prey and keeps after it while the
// targeting conditions hold.
type nonTameRandomTargetGoal struct{ *NearestAttackableTargetGoal }

func newNonTameRandomTargetGoal(isType func(world.Entity) bool, mustSee bool, selector func(*Mob, world.Entity) bool) nonTameRandomTargetGoal {
	g := NewNearestAttackableTargetGoal(isType, mustSee)
	g.Selector = selector
	return nonTameRandomTargetGoal{g}
}

func (g nonTameRandomTargetGoal) CanUse(m *Mob) bool {
	return !tamableOf(m).tamed && g.NearestAttackableTargetGoal.CanUse(m)
}

func (g nonTameRandomTargetGoal) CanContinueToUse(m *Mob) bool {
	if g.target == nil {
		return false
	}
	target, ok := g.target.Entity(m.tx)
	return ok && g.conditions(m).test(m, target)
}
