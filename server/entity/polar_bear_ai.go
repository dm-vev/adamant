package entity

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports the goals of Java's PolarBear.

func (s *polarBearState) mobAI() *MobAI { return &s.ai }

func polarBearOf(m *Mob) *polarBearState { return m.Behaviour().(*polarBearState) }

// registerGoals ports PolarBear.registerGoals.
func (s *polarBearState) registerGoals(m *Mob) {
	ai := m.AI
	ai.FollowRange = 20
	ai.Goals.Add(0, NewFloatGoal(m))
	melee := NewMeleeAttackGoal(1.25, true)
	melee.CheckAttack = s.checkAndPerformAttack
	ai.Goals.Add(1, polarBearMeleeAttackGoal{melee, s})
	// Cubs panic from any danger, adults only from fire and other hazards.
	s.panic = NewPanicGoal(2)
	s.panic.Causes = func(src world.DamageSource) bool {
		if s.age < 0 {
			return panicCause(src)
		}
		return panicEnvironmentalCause(src)
	}
	ai.Goals.Add(1, s.panic)
	ai.Goals.Add(4, NewFollowParentGoal(1.25))
	ai.Goals.Add(5, NewRandomStrollGoal(1))
	ai.Goals.Add(6, NewLookAtPlayerGoal(6))
	ai.Goals.Add(7, NewRandomLookAroundGoal())
	hurt := NewHurtByTargetGoal()
	hurt.AlertIgnore = func(e world.Entity) bool { b, ok := e.(*PolarBear); return !ok || b.b().age < 0 }
	ai.Targets.Add(1, polarBearHurtByTargetGoal{hurt, s})
	players := &NearestAttackableTargetGoal{targetGoal: newTargetGoal(true, true), RandomInterval: reducedTickDelay(20)}
	players.followScale = .5
	ai.Targets.Add(2, polarBearAttackPlayersGoal{players, s})
	angry := NewNearestAttackableTargetGoal(nil, true)
	angry.Selector = func(m *Mob, target world.Entity) bool { return s.angryAt(m, target) }
	ai.Targets.Add(3, angry)
	foxes := &NearestAttackableTargetGoal{targetGoal: newTargetGoal(true, true), RandomInterval: reducedTickDelay(10),
		IsType:   func(e world.Entity) bool { return e.H().Type() == FoxType },
		Selector: func(*Mob, world.Entity) bool { return s.age >= 0 }}
	ai.Targets.Add(4, foxes)
	ai.Targets.Add(5, &resetUniversalAngerTargetGoal{lastHurtByPlayer: -1})
}

// checkAndPerformAttack ports PolarBearMeleeAttackGoal.checkAndPerformAttack:
// a polar bear in reach swipes its target, and one close to it rears up and
// growls half a second before it may swipe.
func (s *polarBearState) checkAndPerformAttack(m *Mob, g *MeleeAttackGoal, target world.Entity) {
	switch {
	case g.CanPerformAttack(m, target):
		g.ResetAttackCooldown()
		m.doHurtTarget(target)
		s.standing = false
	case m.DistanceToSqr(target) < (target.H().Type().BBox(target).Width()+3)*(target.H().Type().BBox(target).Width()+3):
		if g.TicksUntilNextAttack() <= 0 {
			s.standing = false
			g.ResetAttackCooldown()
		}
		if g.TicksUntilNextAttack() <= 10 {
			s.standing = true
			s.playWarningSound(m)
		}
	default:
		g.ResetAttackCooldown()
		s.standing = false
	}
}

// playWarningSound ports PolarBear.playWarningSound: the growl repeats at
// most every two seconds.
func (s *polarBearState) playWarningSound(m *Mob) {
	if s.warningSoundTicks <= 0 {
		m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.polar_bear.warning", Volume: 1, Pitch: 1})
		s.warningSoundTicks = 40
	}
}

// doHurtTarget ports Mob.doHurtTarget for polar bears, which swipe for 6
// damage.
func (s *polarBearState) doHurtTarget(m *Mob, target world.Entity) bool {
	l, ok := target.(Living)
	if !ok {
		return false
	}
	if _, hurt := l.Hurt(6, AttackDamageSource{Attacker: m.Self}); !hurt {
		return false
	}
	l.KnockBack(m.data.Pos, .4, .4)
	return true
}

// waterSlowDown ports PolarBear.getWaterSlowDown: polar bears swim well.
func (s *polarBearState) waterSlowDown() float64 { return .98 }

// polarBearMeleeAttackGoal ports PolarBearMeleeAttackGoal, which lowers the
// bear when it stops.
type polarBearMeleeAttackGoal struct {
	*MeleeAttackGoal
	bear *polarBearState
}

func (g polarBearMeleeAttackGoal) Stop(m *Mob) {
	g.bear.standing = false
	g.MeleeAttackGoal.Stop(m)
}

// polarBearHurtByTargetGoal ports PolarBearHurtByTargetGoal: a hurt cub
// calls the adults around to its aid and flees.
type polarBearHurtByTargetGoal struct {
	*HurtByTargetGoal
	bear *polarBearState
}

func (g polarBearHurtByTargetGoal) Start(m *Mob) {
	g.HurtByTargetGoal.Start(m)
	if g.bear.age < 0 {
		if attacker, _ := m.LastHurtBy(); attacker != nil {
			g.alertOthers(m, attacker)
		}
		g.HurtByTargetGoal.Stop(m)
	}
}

// polarBearAttackPlayersGoal ports PolarBearAttackPlayersGoal: an adult with
// a cub within eight blocks attacks players within half its follow range
// it can reach.
type polarBearAttackPlayersGoal struct {
	*NearestAttackableTargetGoal
	bear *polarBearState
}

func (g polarBearAttackPlayersGoal) CanUse(m *Mob) bool {
	if g.bear.age < 0 || !g.NearestAttackableTargetGoal.CanUse(m) {
		return false
	}
	for e := range m.tx.EntitiesWithin(m.BBox().GrowVec3(mgl64.Vec3{8, 4, 8})) {
		if b, ok := e.(*PolarBear); ok && b.b().age < 0 {
			return true
		}
	}
	return false
}
