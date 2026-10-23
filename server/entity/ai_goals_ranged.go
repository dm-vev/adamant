package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// mobBowUser is implemented by behaviours of mobs that draw a bow, as Java's
// RangedAttackMob holding a bow.
type mobBowUser interface {
	holdingBow() bool
	usingItem() bool
	startUsingItem(m *Mob)
	stopUsingItem(m *Mob)
	ticksUsingItem() int
	performRangedAttack(m *Mob, target world.Entity, power float64)
}

// RangedBowAttackGoal draws the bow of a mob at its target and strafes
// around it, ported from Java's RangedBowAttackGoal.
type RangedBowAttackGoal struct {
	goalFlags
	SpeedModifier     float64
	AttackIntervalMin int
	attackRadiusSqr   float64

	attackTime        int
	seeTime           int
	strafingClockwise bool
	strafingBackwards bool
	strafingTime      int
}

// NewRangedBowAttackGoal returns a RangedBowAttackGoal that shoots from at
// most attackRadius blocks away.
func NewRangedBowAttackGoal(speedModifier float64, attackIntervalMin int, attackRadius float64) *RangedBowAttackGoal {
	return &RangedBowAttackGoal{goalFlags: goalFlags{GoalMove | GoalLook}, SpeedModifier: speedModifier, AttackIntervalMin: attackIntervalMin,
		attackRadiusSqr: attackRadius * attackRadius, attackTime: -1, strafingTime: -1}
}

func bowUser(m *Mob) mobBowUser {
	u, _ := m.Behaviour().(mobBowUser)
	return u
}

func (g *RangedBowAttackGoal) CanUse(m *Mob) bool {
	u := bowUser(m)
	return m.Target() != nil && u != nil && u.holdingBow()
}

func (g *RangedBowAttackGoal) CanContinueToUse(m *Mob) bool {
	u := bowUser(m)
	return (g.CanUse(m) || !m.AI.nav.IsDone()) && u != nil && u.holdingBow()
}

func (g *RangedBowAttackGoal) Start(m *Mob) { m.SetAggressive(true) }

func (g *RangedBowAttackGoal) Stop(m *Mob) {
	m.SetAggressive(false)
	g.seeTime, g.attackTime = 0, -1
	if u := bowUser(m); u != nil {
		u.stopUsingItem(m)
	}
}

func (g *RangedBowAttackGoal) RequiresUpdateEveryTick() bool { return true }

func (g *RangedBowAttackGoal) Tick(m *Mob) {
	target := m.Target()
	u := bowUser(m)
	if target == nil || u == nil {
		return
	}
	dist := m.DistanceToSqr(target)
	sight := m.HasLineOfSight(target)
	if sight != (g.seeTime > 0) {
		g.seeTime = 0
	}
	if sight {
		g.seeTime++
	} else {
		g.seeTime--
	}
	if dist <= g.attackRadiusSqr && g.seeTime >= 20 {
		m.AI.nav.Stop()
		g.strafingTime++
	} else {
		m.AI.nav.MoveToEntity(m, target, g.SpeedModifier)
		g.strafingTime = -1
	}
	r := m.Random()
	if g.strafingTime >= 20 {
		if r.NextFloat() < .3 {
			g.strafingClockwise = !g.strafingClockwise
		}
		if r.NextFloat() < .3 {
			g.strafingBackwards = !g.strafingBackwards
		}
		g.strafingTime = 0
	}
	if g.strafingTime > -1 {
		if dist > g.attackRadiusSqr*.75 {
			g.strafingBackwards = false
		} else if dist < g.attackRadiusSqr*.25 {
			g.strafingBackwards = true
		}
		forwards, right := .5, -.5
		if g.strafingBackwards {
			forwards = -.5
		}
		if g.strafingClockwise {
			right = .5
		}
		m.AI.move.Strafe(forwards, right)
		m.LookAt(target, 30, 30)
	} else {
		m.AI.look.SetLookAtEntityWith(target, 30, 30)
	}
	if u.usingItem() {
		if !sight && g.seeTime < -60 {
			u.stopUsingItem(m)
		} else if sight {
			if pull := u.ticksUsingItem(); pull >= 20 {
				u.stopUsingItem(m)
				u.performRangedAttack(m, target, bowPowerForTime(pull))
				g.attackTime = g.AttackIntervalMin
			}
		}
	} else if g.attackTime--; g.attackTime <= 0 && g.seeTime >= -60 {
		u.startUsingItem(m)
	}
}

// bowPowerForTime ports BowItem.getPowerForTime.
func bowPowerForTime(ticks int) float64 {
	f := float64(ticks) / 20
	f = (f*f + f*2) / 3
	return math.Min(f, 1)
}

// shootDirection returns the velocity a mob shoots a projectile at, ported
// from Projectile.getMovementToShoot: the direction spread by the
// uncertainty, scaled by pow.
func shootDirection(m *Mob, d mgl64.Vec3, pow, uncertainty float64) mgl64.Vec3 {
	if d.Len() == 0 {
		return mgl64.Vec3{}
	}
	r := m.Random()
	spread := .0172275 * uncertainty
	triangle := func() float64 { return spread * (r.NextDouble() - r.NextDouble()) }
	return d.Normalize().Add(mgl64.Vec3{triangle(), triangle(), triangle()}).Mul(pow)
}

// rangedAttackUncertainty ports RangedAttackMob.rangedAttackUncertainty:
// mobs aim better on higher difficulties.
func rangedAttackUncertainty(m *Mob) float64 {
	id, _ := world.DifficultyID(m.tx.World().Difficulty())
	return float64(14 - id*4)
}

// AvoidEntityGoal walks a mob away from entities it fears, ported from
// Java's AvoidEntityGoal.
type AvoidEntityGoal struct {
	goalFlags
	IsType      func(world.Entity) bool
	Predicate   func(world.Entity) bool
	MaxDist     float64
	WalkSpeed   float64
	SprintSpeed float64

	toAvoid *world.EntityHandle
	path    *mobPath
}

// NewAvoidEntityGoal returns an AvoidEntityGoal for the entities isType
// selects within maxDist blocks.
func NewAvoidEntityGoal(isType func(world.Entity) bool, maxDist, walkSpeed, sprintSpeed float64) *AvoidEntityGoal {
	return &AvoidEntityGoal{goalFlags: goalFlags{GoalMove}, IsType: isType, MaxDist: maxDist, WalkSpeed: walkSpeed, SprintSpeed: sprintSpeed}
}

func (g *AvoidEntityGoal) CanUse(m *Mob) bool {
	c := forCombat()
	c.rangeLimit = g.MaxDist
	c.selector = func(_ *Mob, target world.Entity) bool {
		return !creativeOrSpectator(target) && (g.Predicate == nil || g.Predicate(target))
	}
	box := m.BBox().GrowVec3(mgl64.Vec3{g.MaxDist, 3, g.MaxDist})
	var avoid world.Entity
	bestDist := -1.0
	for e := range m.tx.EntitiesWithin(box) {
		if !g.IsType(e) || !c.test(m, e) {
			continue
		}
		if d := e.Position().Sub(m.data.Pos).LenSqr(); bestDist == -1 || d < bestDist {
			avoid, bestDist = e, d
		}
	}
	if avoid == nil {
		return false
	}
	pos, ok := DefaultRandomPosAway(m, 16, 7, avoid.Position())
	if !ok || avoid.Position().Sub(pos).LenSqr() < avoid.Position().Sub(m.data.Pos).LenSqr() {
		return false
	}
	g.path = m.AI.nav.CreatePath(m, cube.PosFromVec3(pos), 0)
	if g.path == nil {
		return false
	}
	g.toAvoid = avoid.H()
	return true
}

func (g *AvoidEntityGoal) CanContinueToUse(m *Mob) bool { return !m.AI.nav.IsDone() }

func (g *AvoidEntityGoal) Start(m *Mob) { m.AI.nav.MoveTo(m, g.path, g.WalkSpeed) }

func (g *AvoidEntityGoal) Stop(*Mob) { g.toAvoid = nil }

func (g *AvoidEntityGoal) Tick(m *Mob) {
	avoid, ok := g.toAvoid.Entity(m.tx)
	if ok && m.DistanceToSqr(avoid) < 49 {
		m.AI.nav.SetSpeedModifier(g.SprintSpeed)
	} else {
		m.AI.nav.SetSpeedModifier(g.WalkSpeed)
	}
}

// mobHeadEquipment is implemented by behaviours of mobs that wear something
// on their head, which keeps the sun off undead.
type mobHeadEquipment interface {
	headEmpty() bool
}

func headEmpty(m *Mob) bool {
	if h, ok := m.Behaviour().(mobHeadEquipment); ok {
		return h.headEmpty()
	}
	return true
}

// RestrictSunGoal keeps the paths of a mob in the shade by day, ported from
// Java's RestrictSunGoal.
type RestrictSunGoal struct{ goalFlags }

func (g *RestrictSunGoal) CanUse(m *Mob) bool { return brightOutside(m.tx) && headEmpty(m) }

func (g *RestrictSunGoal) Start(m *Mob) { m.AI.nav.AvoidSun = true }

func (g *RestrictSunGoal) Stop(m *Mob) { m.AI.nav.AvoidSun = false }

// FleeSunGoal walks a burning mob into the shade, ported from Java's
// FleeSunGoal.
type FleeSunGoal struct {
	goalFlags
	SpeedModifier float64
	wanted        mgl64.Vec3
}

// NewFleeSunGoal returns a FleeSunGoal.
func NewFleeSunGoal(speedModifier float64) *FleeSunGoal {
	return &FleeSunGoal{goalFlags: goalFlags{GoalMove}, SpeedModifier: speedModifier}
}

func (g *FleeSunGoal) CanUse(m *Mob) bool {
	if m.Target() != nil || !brightOutside(m.tx) || m.OnFireDuration() <= 0 || !canSeeSky(m.tx, m.BlockPos()) || !headEmpty(m) {
		return false
	}
	pos, ok := g.hidePos(m)
	if ok {
		g.wanted = pos
	}
	return ok
}

func (g *FleeSunGoal) hidePos(m *Mob) (mgl64.Vec3, bool) {
	r := m.Random()
	origin := m.BlockPos()
	for range 10 {
		pos := origin.Add(cube.Pos{int(r.NextInt(20)) - 10, int(r.NextInt(6)) - 3, int(r.NextInt(20)) - 10})
		if !canSeeSky(m.tx, pos) && m.walkTargetValue(pos) < 0 {
			return mgl64.Vec3{float64(pos.X()) + .5, float64(pos.Y()), float64(pos.Z()) + .5}, true
		}
	}
	return mgl64.Vec3{}, false
}

func (g *FleeSunGoal) CanContinueToUse(m *Mob) bool { return !m.AI.nav.IsDone() }

func (g *FleeSunGoal) Start(m *Mob) {
	m.AI.nav.MoveToPos(m, g.wanted[0], g.wanted[1], g.wanted[2], g.SpeedModifier)
}
