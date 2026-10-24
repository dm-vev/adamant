package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports the use of items by mobs as far as kinetic weapons need
// it: LivingEntity.startUsingItem and updatingUsingItem, KineticWeapon and
// the spear goals and behaviours of zombies and piglins.

// mainHand returns what the mob holds in its main hand.
func (m *Mob) mainHand() item.Stack {
	if u, ok := m.Self.(interface {
		HeldItems() (item.Stack, item.Stack)
	}); ok {
		main, _ := u.HeldItems()
		return main
	}
	return item.Stack{}
}

// StartUsingItem ports LivingEntity.startUsingItem for the main hand.
func (m *Mob) StartUsingItem() {
	held := m.mainHand()
	if held.Empty() || m.UsingItem() {
		return
	}
	m.AI.useItem, m.AI.useTicks = held, 0
	m.updateState()
}

// StopUsingItem ports LivingEntity.stopUsingItem.
func (m *Mob) StopUsingItem() {
	if !m.UsingItem() {
		return
	}
	m.AI.useItem, m.AI.useTicks = item.Stack{}, 0
	m.updateState()
}

// UsingItem reports if the mob uses the item in its main hand.
func (m *Mob) UsingItem() bool { return !m.AI.useItem.Empty() }

// tickUsingItem ports LivingEntity.updatingUsingItem: a mob that no longer
// holds what it used stops, and a kinetic weapon in use stabs.
func (ai *MobAI) tickUsingItem(m *Mob) {
	if !m.UsingItem() {
		return
	}
	held := m.mainHand()
	if held.Empty() || !sameItem(held, ai.useItem) {
		m.StopUsingItem()
		return
	}
	if k, ok := held.Item().(item.KineticWeapon); ok {
		mobKineticStab(m, k.KineticWeaponInfo(), ai.useTicks)
	}
	ai.useTicks++
}

// sameItem ports ItemStack.isSameItem: the same item, whatever its data.
func sameItem(a, b item.Stack) bool {
	an, _ := a.Item().EncodeItem()
	bn, _ := b.Item().EncodeItem()
	return an == bn
}

// knownSpeed ports Entity.getKnownSpeed: how far the entity moved in the last
// tick.
func knownSpeed(e world.Entity) mgl64.Vec3 {
	if m, ok := mobOf(e); ok && m.AI.started {
		return m.AI.knownSpeed
	}
	if v, ok := e.(interface{ Velocity() mgl64.Vec3 }); ok {
		return v.Velocity()
	}
	return mgl64.Vec3{}
}

// spearAttackRange is the AttackRange of spears: a mob stabs from half of 2
// to half of 4.5 blocks ahead of its eyes.
var spearAttackRange = struct{ minReach, maxReach, margin, mobFactor float64 }{2, 4.5, .125, .5}

// mobKineticStab ports KineticWeapon.damageEntities for a mob: after the
// delay of the weapon, every entity along its reach that it did not stab
// lately is dismounted, knocked back or hurt, as fast as the mob moves into
// it allows.
func mobKineticStab(m *Mob, info item.KineticWeaponInfo, ticksUsed int) {
	if ticksUsed < info.DelayTicks {
		return
	}
	ticksUsed -= info.DelayTicks
	look := m.data.Rot.Vec3()
	attackerSpeed := look.Dot(knownSpeed(m.Self).Mul(20))
	const factor = .2
	eye := mgl64.Vec3{m.data.Pos[0], m.EyeY(), m.data.Pos[2]}
	r := spearAttackRange
	from := eye.Add(look.Mul(r.minReach * r.mobFactor))
	to := eye.Add(look.Mul(r.maxReach*r.mobFactor + math.Max(0, m.data.Vel.Dot(look))))
	blocked := false
	trace.TraverseBlocks(eye, to, func(pos cube.Pos) bool {
		if res, ok := trace.BlockIntercept(pos, m.tx, m.tx.Block(pos), eye, to, trace.CollisionShape); ok {
			to = res.Position()
			blocked = eye.Sub(to).LenSqr() < eye.Sub(from).LenSqr()
			return false
		}
		return true
	})
	if blocked {
		return
	}
	if m.AI.stabbed == nil {
		m.AI.stabbed = map[*world.EntityHandle]int64{}
	}
	now := m.tx.CurrentTick()
	box := cube.Box(math.Min(from[0], to[0]), math.Min(from[1], to[1]), math.Min(from[2], to[2]), math.Max(from[0], to[0]), math.Max(from[1], to[1]), math.Max(from[2], to[2])).Grow(r.margin + 1)
	base := 2.0
	if b, ok := m.Behaviour().(interface{ attackDamageBase() float64 }); ok {
		base = b.attackDamageBase()
	}
	affected := false
	for target := range m.tx.EntitiesWithin(box.Grow(2)) {
		if target.H() == m.H() || !CanBeHitByPiercingWeapon(target, m.Self) {
			continue
		}
		if _, ok := trace.BBoxIntercept(target.H().Type().BBox(target).Translate(target.Position()).Grow(r.margin), from, to); !ok {
			continue
		}
		if last, ok := m.AI.stabbed[target.H()]; ok && now-last < int64(info.ContactCooldownTicks) {
			continue
		}
		m.AI.stabbed[target.H()] = now
		targetSpeed := look.Dot(knownSpeed(target).Mul(20))
		relative := math.Max(0, attackerSpeed-targetSpeed)
		dismount := info.Dismount.Matches(ticksUsed, attackerSpeed, relative, factor)
		knockback := info.Knockback.Matches(ticksUsed, attackerSpeed, relative, factor)
		damage := info.Damage.Matches(ticksUsed, attackerSpeed, relative, factor)
		if dismount || knockback || damage {
			affected = mobStabAttack(m, target, base+math.Floor(relative*info.DamageMultiplier), damage, knockback, dismount) || affected
		}
	}
	for h, last := range m.AI.stabbed {
		if now-last > int64(info.ContactCooldownTicks) {
			delete(m.AI.stabbed, h)
		}
	}
	if affected {
		m.swing()
	}
}

// mobStabAttack ports LivingEntity.stabAttack.
func mobStabAttack(m *Mob, target world.Entity, damage float64, dealsDamage, dealsKnockback, dismounts bool) bool {
	affected := dealsKnockback
	l, living := target.(Living)
	if dealsDamage && living {
		_, hurt := l.Hurt(damage, SpearDamageSource{AttackDamageSource: AttackDamageSource{Attacker: m.Self}})
		affected = affected || hurt
	}
	if dealsKnockback && living {
		yaw := m.Yaw() * math.Pi / 180
		l.KnockBack(target.Position().Sub(mgl64.Vec3{-math.Sin(yaw), 0, math.Cos(yaw)}), .4, .4)
	}
	if dismounts && DismountRider(target, m.tx) {
		affected = true
	}
	if !affected {
		return false
	}
	m.AI.setLastHurtMob(target)
	return true
}

// kineticUseDuration ports KineticWeapon.computeDamageUseDuration.
func kineticUseDuration(s item.Stack) int {
	k, ok := s.Item().(item.KineticWeapon)
	if !ok {
		return 0
	}
	info := k.KineticWeaponInfo()
	return info.DelayTicks + info.Damage.MaxDurationTicks
}

// holdsKineticWeapon reports if the mob holds a kinetic weapon.
func holdsKineticWeapon(m *Mob) bool {
	_, ok := m.mainHand().Item().(item.KineticWeapon)
	return ok
}

// LandRandomPosAwayRange ports LandRandomPos.getPosAway with a range of
// distances.
func LandRandomPosAwayRange(m *Mob, minHorizontal, maxHorizontal float64, vertical int, away mgl64.Vec3) (mgl64.Vec3, bool) {
	dir := m.data.Pos.Sub(away)
	if dir.Len() == 0 {
		r := m.Random()
		dir = mgl64.Vec3{r.NextDouble() - .5, 0, r.NextDouble() - .5}
	}
	return landRandomPosInDirection(m, minHorizontal, maxHorizontal, vertical, dir)
}

// spearUseGoal ports SpearUseGoal: a zombie with a spear walks up to its
// target, lowers its spear and charges it, runs past to a spot six or seven
// blocks off, charges again until its spear is up, and backs off to nine to
// eleven blocks away.
type spearUseGoal struct {
	goalFlags
	charging, repositioning float64
	approachSqr, rangeSqr   float64

	active               bool
	engageTime, fleeTime int
	awayPos              *mgl64.Vec3
	done                 bool
}

func newSpearUseGoal(charging, repositioning, approach, inRange float64) *spearUseGoal {
	return &spearUseGoal{goalFlags: goalFlags{GoalMove | GoalLook}, charging: charging, repositioning: repositioning, approachSqr: approach * approach, rangeSqr: inRange * inRange}
}

func (g *spearUseGoal) ableToAttack(m *Mob) bool { return m.Target() != nil && holdsKineticWeapon(m) }

func (g *spearUseGoal) CanUse(m *Mob) bool { return g.ableToAttack(m) && !m.UsingItem() }

func (g *spearUseGoal) CanContinueToUse(m *Mob) bool {
	return g.active && !g.done && g.ableToAttack(m)
}

func (g *spearUseGoal) Start(m *Mob) {
	m.SetAggressive(true)
	g.active, g.engageTime, g.fleeTime, g.awayPos, g.done = true, -1, -1, nil, false
}

func (g *spearUseGoal) Stop(m *Mob) {
	m.AI.nav.Stop()
	m.SetAggressive(false)
	g.active = false
	m.StopUsingItem()
}

func (g *spearUseGoal) RequiresUpdateEveryTick() bool { return true }

func (g *spearUseGoal) Tick(m *Mob) {
	if !g.active {
		return
	}
	target := m.Target()
	if target == nil {
		return
	}
	dist := m.DistanceToSqr(target)
	mount := 0.0
	if m.data.Riding != nil {
		mount = 2
	}
	m.LookAt(target, 30, 30)
	m.AI.look.SetLookAtEntityWith(target, 30, 30)
	if g.engageTime < 0 {
		if dist > g.approachSqr {
			m.AI.nav.MoveToEntity(m, target, g.repositioning)
			return
		}
		g.engageTime = reducedTickDelay(kineticUseDuration(m.mainHand()))
		m.StartUsingItem()
	}
	if g.engageTime > 0 {
		if g.engageTime--; g.engageTime == 0 {
			m.StopUsingItem()
			d := math.Sqrt(dist)
			if p, ok := LandRandomPosAwayRange(m, math.Max(0, 9+mount-d), math.Max(1, 11+mount-d), 7, target.Position()); ok {
				g.awayPos = &p
			} else {
				g.awayPos = nil
			}
			g.fleeTime = 1
		}
	}
	if g.fleeTime > 0 {
		if g.fleeTime++; float64(g.fleeTime) > float64(reducedTickDelay(100)) {
			g.done = true
			return
		}
	}
	if g.awayPos != nil {
		m.AI.nav.MoveToPos(m, g.awayPos[0], g.awayPos[1], g.awayPos[2], g.repositioning)
		if m.AI.nav.IsDone() {
			if g.fleeTime > 0 {
				g.done = true
				return
			}
			g.awayPos = nil
		}
		return
	}
	m.AI.nav.MoveToEntity(m, target, g.charging)
	if dist < g.rangeSqr || m.AI.nav.IsDone() {
		d := math.Sqrt(dist)
		if p, ok := LandRandomPosAwayRange(m, 6+mount-d, 7+mount-d, 7, target.Position()); ok {
			g.awayPos = &p
		}
	}
}

// spearStatus is SpearAttack.SpearStatus.
type spearStatus uint8

const (
	spearApproach spearStatus = iota
	spearCharging
	spearRetreat
)

// spearAbleToAttack reports if a mob with a brain has an attack target and
// a kinetic weapon.
func spearAbleToAttack(m *Mob) bool {
	return m.Brain().has(memAttackTarget) && holdsKineticWeapon(m)
}

// newSpearApproach ports SpearApproach: a mob with a spear walks to within
// approach of its target.
func newSpearApproach(speed, approach float64) *behavior {
	return &behavior{
		entry:      []memoryCondition{{memSpearStatus, memAbsent}},
		noTimeout:  true,
		checkExtra: func(m *Mob) bool { return spearAbleToAttack(m) && !m.UsingItem() },
		canStillUse: func(m *Mob, _ int64) bool {
			target := m.Brain().entity(m, memAttackTarget)
			return spearAbleToAttack(m) && target != nil && m.DistanceToSqr(target) > approach*approach
		},
		start: func(m *Mob, _ int64) {
			m.SetAggressive(true)
			m.Brain().set(memSpearStatus, spearApproach)
		},
		tick: func(m *Mob, _ int64) {
			target := m.Brain().entity(m, memAttackTarget)
			if target == nil {
				return
			}
			m.Brain().set(memLookTarget, newEntityTracker(target, true))
			m.AI.nav.MoveToEntity(m, target, speed)
		},
		stop: func(m *Mob, _ int64) {
			m.AI.nav.Stop()
			m.Brain().set(memSpearStatus, spearCharging)
		},
	}
}

// newSpearAttack ports SpearAttack: a mob with its spear lowered charges its
// target, runs past it to a spot six or seven blocks off and charges again
// until its spear is up.
func newSpearAttack(charging, repositioning, inRange float64) *behavior {
	return &behavior{
		entry:     []memoryCondition{{memSpearStatus, memPresent}},
		noTimeout: true,
		checkExtra: func(m *Mob) bool {
			v, _ := m.Brain().get(memSpearStatus)
			return v == spearCharging && spearAbleToAttack(m) && !m.UsingItem()
		},
		canStillUse: func(m *Mob, _ int64) bool {
			t, _ := m.Brain().int(memSpearEngageTime)
			return t > 0 && spearAbleToAttack(m)
		},
		start: func(m *Mob, _ int64) {
			m.SetAggressive(true)
			m.Brain().set(memSpearEngageTime, kineticUseDuration(m.mainHand()))
			m.Brain().erase(memSpearChargePosition)
			m.StartUsingItem()
		},
		tick: func(m *Mob, _ int64) {
			b := m.Brain()
			target := b.entity(m, memAttackTarget)
			if target == nil {
				return
			}
			dist := m.DistanceToSqr(target)
			mount := 0.0
			if m.data.Riding != nil {
				mount = 2
			}
			b.set(memLookTarget, newEntityTracker(target, true))
			t, _ := b.int(memSpearEngageTime)
			b.set(memSpearEngageTime, t-1)
			if v, ok := b.get(memSpearChargePosition); ok {
				away := v.(mgl64.Vec3)
				m.AI.nav.MoveToPos(m, away[0], away[1], away[2], repositioning)
				if m.AI.nav.IsDone() {
					b.erase(memSpearChargePosition)
				}
				return
			}
			m.AI.nav.MoveToEntity(m, target, charging)
			if dist < inRange*inRange || m.AI.nav.IsDone() {
				d := math.Sqrt(dist)
				if p, ok := LandRandomPosAwayRange(m, 6+mount-d, 7+mount-d, 7, target.Position()); ok {
					b.set(memSpearChargePosition, p)
				} else {
					b.erase(memSpearChargePosition)
				}
			}
		},
		stop: func(m *Mob, _ int64) {
			m.AI.nav.Stop()
			m.StopUsingItem()
			b := m.Brain()
			b.erase(memSpearChargePosition)
			b.erase(memSpearEngageTime)
			b.set(memSpearStatus, spearRetreat)
		},
	}
}

// newSpearRetreat ports SpearRetreat: a mob whose spear is up backs off to
// nine to eleven blocks from its target for up to five seconds.
func newSpearRetreat(speed float64) *behavior {
	return &behavior{
		entry:       []memoryCondition{{memSpearStatus, memPresent}},
		minDuration: 100, maxDuration: 100,
		checkExtra: func(m *Mob) bool {
			b := m.Brain()
			if !spearAbleToAttack(m) || m.UsingItem() {
				return false
			}
			if v, _ := b.get(memSpearStatus); v != spearRetreat {
				return false
			}
			target := b.entity(m, memAttackTarget)
			if target == nil {
				return false
			}
			mount := 0.0
			if m.data.Riding != nil {
				mount = 2
			}
			d := math.Sqrt(m.DistanceToSqr(target))
			p, ok := LandRandomPosAwayRange(m, math.Max(0, 9+mount-d), math.Max(1, 11+mount-d), 7, target.Position())
			if !ok {
				return false
			}
			b.set(memSpearFleeingPosition, p)
			return true
		},
		canStillUse: func(m *Mob, _ int64) bool {
			b := m.Brain()
			t, ok := b.int(memSpearFleeingTime)
			if !ok {
				t = 100
			}
			return t < 100 && b.has(memSpearFleeingPosition) && !m.AI.nav.IsDone() && spearAbleToAttack(m)
		},
		start: func(m *Mob, _ int64) {
			m.SetAggressive(true)
			m.Brain().set(memSpearFleeingTime, 0)
		},
		tick: func(m *Mob, _ int64) {
			b := m.Brain()
			target := b.entity(m, memAttackTarget)
			if target == nil {
				return
			}
			b.set(memLookTarget, newEntityTracker(target, true))
			t, _ := b.int(memSpearFleeingTime)
			b.set(memSpearFleeingTime, t+1)
			if v, ok := b.get(memSpearFleeingPosition); ok {
				p := v.(mgl64.Vec3)
				m.AI.nav.MoveToPos(m, p[0], p[1], p[2], speed)
			}
		},
		stop: func(m *Mob, _ int64) {
			m.AI.nav.Stop()
			m.SetAggressive(false)
			m.StopUsingItem()
			b := m.Brain()
			b.erase(memSpearFleeingTime)
			b.erase(memSpearFleeingPosition)
			b.erase(memSpearStatus)
		},
	}
}
