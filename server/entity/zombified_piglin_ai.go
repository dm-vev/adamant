package entity

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports the goals of Java's ZombifiedPiglin, a zombie that is a
// neutral mob.

func (s *zombifiedPiglinState) mobAI() *MobAI { return &s.ai }

func zombifiedPiglinOf(m *Mob) *zombifiedPiglinState { return m.Behaviour().(*zombifiedPiglinState) }

// Tick runs the MobAI of a zombified piglin.
func (s *zombifiedPiglinState) Tick(e *Ent, tx *world.Tx) *Movement {
	ai := &s.ai
	ai.MovementSpeed, ai.FollowRange, ai.EyeHeight = s.speed, 35, 1.79
	if s.baby {
		ai.EyeHeight = .78
	} else if s.angry() {
		// SPEED_MODIFIER_ATTACKING of 0.05.
		ai.MovementSpeed += .05
	}
	return ai.Tick(e, &ZombifiedPiglin{e}, s.registerGoals)
}

// registerGoals ports Zombie.registerGoals with the behaviour goals of
// ZombifiedPiglin.
func (s *zombifiedPiglinState) registerGoals(m *Mob) {
	ai := m.AI
	ai.SetPathfindingMalus(PathLava, 8)
	ai.Goals.Add(8, NewLookAtPlayerGoal(8))
	ai.Goals.Add(8, NewRandomLookAroundGoal())
	attack := NewMeleeAttackGoal(1, false)
	raiseArmTicks := 0
	attack.OnTick = func(m *Mob, g *MeleeAttackGoal) {
		raiseArmTicks++
		m.SetAggressive(raiseArmTicks >= 5 && g.TicksUntilNextAttack() < g.AttackInterval()/2)
	}
	ai.Goals.Add(1, newSpearUseGoal(1, 1, 10, 2))
	ai.Goals.Add(2, &zombieAttackGoal{MeleeAttackGoal: attack, raiseArmTicks: &raiseArmTicks})
	ai.Goals.Add(7, NewWaterAvoidingRandomStrollGoal(1))
	hurt := NewHurtByTargetGoal()
	hurt.AlertOthers = true
	ai.Targets.Add(1, hurt)
	players := NewNearestAttackableTargetGoal(nil, true)
	players.Selector = func(m *Mob, target world.Entity) bool { return zombifiedPiglinOf(m).angryAt(m, target) }
	ai.Targets.Add(2, players)
	ai.Targets.Add(3, &resetUniversalAngerTargetGoal{lastHurtByPlayer: -1, alertSameType: true})
}

// targetSet ports ZombifiedPiglin.setTarget: a piglin that finds a first
// target soon makes an angry sound and alerts others.
func (s *zombifiedPiglinState) targetSet(m *Mob, target world.Entity) {
	if m.Target() == nil && target != nil {
		r := m.Random()
		s.firstAngerSoundIn = int(r.NextInt(21))
		s.alertIn = 80 + int(r.NextInt(41))
	}
}

// customServerAiStep ports ZombifiedPiglin.customServerAiStep: an angry
// piglin roars once, stays angry while it has a target and calls the
// piglins around every four to six seconds.
func (s *zombifiedPiglinState) customServerAiStep(m *Mob) {
	if s.angry() && s.firstAngerSoundIn > 0 {
		if s.firstAngerSoundIn--; s.firstAngerSoundIn == 0 {
			r := m.Random()
			pitch := (float64(r.NextFloat()-r.NextFloat())*.2 + 1) * 1.8
			if s.baby {
				pitch = (float64(r.NextFloat()-r.NextFloat())*.2 + 1.5) * 1.8
			}
			m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.zombified_piglin.angry", Volume: 2, Pitch: pitch})
		}
	}
	s.updatePersistentAnger(m, true)
	target := m.Target()
	if target == nil {
		return
	}
	if s.alertIn > 0 {
		s.alertIn--
		return
	}
	if m.HasLineOfSight(target) {
		zombifiedPiglinAlertOthers(m, target)
	}
	s.alertIn = 80 + int(m.Random().NextInt(41))
}

// zombifiedPiglinAlertOthers ports ZombifiedPiglin.alertOthers: the piglins
// within follow range without a target take this one's.
func zombifiedPiglinAlertOthers(m *Mob, target world.Entity) {
	r := m.AI.FollowRange
	box := m.BBox().GrowVec3(mgl64.Vec3{r, 10, r})
	for e := range m.tx.EntitiesWithin(box) {
		if _, ok := e.(*ZombifiedPiglin); !ok || e.H() == m.H() || spectator(e) {
			continue
		}
		other, ok := mobOf(e)
		if !ok || other.Target() != nil || alliedTo(other, target) {
			continue
		}
		other.SetTarget(target)
	}
}

// doHurtTarget ports Zombie.doHurtTarget for a zombified piglin.
func (s *zombifiedPiglinState) doHurtTarget(m *Mob, target world.Entity) bool {
	l, ok := target.(Living)
	if !ok {
		return false
	}
	damage := 5.0
	if w, ok := s.main.Item().(item.Weapon); ok {
		damage += w.AttackDamage() - 1
	}
	if _, hurt := l.Hurt(damage, AttackDamageSource{Attacker: m.Self}); !hurt {
		return false
	}
	l.KnockBack(m.data.Pos, .4, .4)
	return true
}

// Angry reports if the zombified piglin is angry.
func (z *ZombifiedPiglin) Angry() bool { return z.z().angry() }

// attackDamageBase returns the attack damage attribute of a zombified piglin.
func (s *zombifiedPiglinState) attackDamageBase() float64 { return 5 }
