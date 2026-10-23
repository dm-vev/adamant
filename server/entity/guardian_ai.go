package entity

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports the goals of Java's Guardian and ElderGuardian.

func guardianOf(m *Mob) *oceanBehaviour { return m.Behaviour().(*oceanBehaviour) }

// guardianAttackDuration ports Guardian.getAttackDuration: the beam of a
// guardian takes four seconds to hurt, that of an elder guardian three.
func (b *oceanBehaviour) guardianAttackDuration() int {
	if b.kind == oceanElderGuardian {
		return 60
	}
	return 80
}

// aiTickGuardian runs the MobAI of a guardian and what Guardian.aiStep does
// before it: out of water a guardian flops about.
func (b *oceanBehaviour) aiTickGuardian(e *Ent, tx *world.Tx) *Movement {
	ai := &b.ai
	ai.MovementSpeed, ai.FollowRange, ai.MaxHeadPitch = b.speed, b.followRange, 180
	ai.EyeHeight = .425
	if b.kind == oceanElderGuardian {
		ai.EyeHeight = .99875
	}
	var self world.Entity = &Guardian{&oceanMob{Ent: e}}
	if b.kind == oceanElderGuardian {
		self = &ElderGuardian{&oceanMob{Ent: e}}
	}
	if ai.started && e.data.Riding == nil {
		m := ai.Mob(e, self)
		if m.InWater() {
			b.air = 300
		} else if m.OnGround() {
			r := m.Random()
			e.data.Vel = e.data.Vel.Add(mgl64.Vec3{float64(r.NextFloat()*2-1) * .4, .5, float64(r.NextFloat()*2-1) * .4})
			m.SetYaw(float64(r.NextFloat()) * 360)
			ai.onGround = false
		}
		if b.beamTarget != nil {
			m.SetYaw(ai.headYaw)
		}
	}
	return ai.Tick(e, self, b.registerGuardianGoals)
}

// registerGuardianGoals ports Guardian.registerGoals.
func (b *oceanBehaviour) registerGuardianGoals(m *Mob) {
	ai := m.AI
	ai.nav.SetWaterBound(false)
	ai.moveOverride = &guardianMoveControl{}
	ai.SetPathfindingMalus(PathWater, 0)
	stroll := NewRandomStrollGoal(1)
	stroll.goalFlags = goalFlags{GoalMove | GoalLook}
	stroll.Interval = 80
	if b.kind == oceanElderGuardian {
		stroll.Interval = 400
	}
	b.stroll = stroll
	ai.Goals.Add(4, &guardianAttackGoal{goalFlags: goalFlags{GoalMove | GoalLook}})
	ai.Goals.Add(5, &moveTowardsRestrictionGoal{goalFlags: goalFlags{GoalMove}, speedModifier: 1})
	ai.Goals.Add(7, stroll)
	ai.Goals.Add(8, NewLookAtPlayerGoal(8))
	lookGuardian := NewLookAtPlayerGoal(12)
	lookGuardian.IsType, lookGuardian.Probability = entityIs("minecraft:guardian", "minecraft:elder_guardian"), .01
	ai.Goals.Add(8, lookGuardian)
	ai.Goals.Add(9, NewRandomLookAroundGoal())
	targets := NewNearestAttackableTargetGoal(func(e world.Entity) bool { _, ok := e.(Living); return ok }, true)
	targets.Selector = func(m *Mob, target world.Entity) bool {
		switch target.H().Type().EncodeEntity() {
		case "minecraft:player", "minecraft:squid", "minecraft:glow_squid", "minecraft:axolotl":
			return m.DistanceToSqr(target) > 9
		}
		return false
	}
	ai.Targets.Add(1, targets)
}

// customServerAiStep ports ElderGuardian.customServerAiStep: every minute an
// elder guardian gives the players within 50 blocks mining fatigue, and it
// stays within 16 blocks of where it started.
func (b *oceanBehaviour) customServerAiStep(m *Mob) {
	if b.kind != oceanElderGuardian {
		return
	}
	if (m.AI.tickCount+int(m.H().UniqueID()&0x7fffffff))%1200 == 0 {
		for p := range m.tx.Players() {
			if p.Position().Sub(m.data.Pos).LenSqr() > 2500 {
				continue
			}
			l, ok := p.(interface {
				AddEffect(effect.Effect)
				Effect(effect.Type) (effect.Effect, bool)
			})
			if !ok || creativeOrSpectator(p) {
				continue
			}
			// MobEffectUtil.addEffectToPlayersAround: players with a strong
			// or long enough mining fatigue keep theirs.
			if e, has := l.Effect(effect.MiningFatigue); has && (e.Level() > 3 || e.Duration() >= time.Minute) {
				continue
			}
			l.AddEffect(effect.New(effect.MiningFatigue, 3, 5*time.Minute))
			if v, ok := p.(interface{ ShowElderGuardianCurse() }); ok {
				v.ShowElderGuardianCurse()
			}
		}
	}
	if !m.AI.home.set() {
		m.SetHome(m.BlockPos(), 16)
	}
}

// walkTargetValue ports Guardian.getWalkTargetValue: guardians swim to
// water, and drowned walk as monsters.
func (b *oceanBehaviour) walkTargetValue(m *Mob, pos cube.Pos) float64 {
	if (b.kind == oceanGuardian || b.kind == oceanElderGuardian) && pathWater(m.tx, pos) {
		return 10 + pathfindingLightCost(m.tx, pos)
	}
	return monsterWalkTarget(m, pos)
}

// guardianTravel ports Guardian.travelInWater: a guardian swims fast and
// slowly sinks while idle.
func (b *oceanBehaviour) guardianTravel(m *Mob, input mgl64.Vec3) bool {
	if !m.InWater() {
		return false
	}
	m.AI.moveRelative(m, .1, input)
	m.AI.moveSelf(m, m.data.Vel)
	m.data.Vel = m.data.Vel.Mul(.9)
	if !b.moving && m.Target() == nil {
		m.data.Vel[1] -= .005
	}
	return true
}

// Moving reports if the guardian swims somewhere, which closes its spikes.
func (m *oceanMob) Moving() bool { return m.b().moving }

// BeamTarget returns the entity the guardian aims its beam at, if any.
func (m *oceanMob) BeamTargetHandle() *world.EntityHandle { return m.b().beamTarget }

func (b *oceanBehaviour) setMoving(m *Mob, moving bool) {
	if b.moving != moving {
		b.moving = moving
		m.updateState()
	}
}

func (b *oceanBehaviour) setBeamTarget(m *Mob, target *world.EntityHandle) {
	if b.beamTarget != target {
		b.beamTarget = target
		m.updateState()
	}
}

// guardianThorns ports the thorns of Guardian.hurtServer: a guardian that
// stands still pricks what hits it, and starts to swim about.
func (b *oceanBehaviour) guardianThorns(self world.Entity, src world.DamageSource) {
	if b.stroll != nil {
		b.stroll.Trigger()
	}
	if b.moving {
		return
	}
	if _, thorns := src.(enchantment.ThornsDamageSource); thorns {
		return
	}
	direct := damageDirectEntity(src)
	if l, ok := direct.(Living); ok {
		l.Hurt(2, enchantment.ThornsDamageSource{Owner: self})
	}
}

// damageDirectEntity returns the entity that dealt damage directly: the
// attacker of a melee hit, none of a projectile.
func damageDirectEntity(src world.DamageSource) world.Entity {
	if a, ok := src.(AttackDamageSource); ok {
		return a.Attacker
	}
	return nil
}

// guardianAttackGoal ports Guardian.GuardianAttackGoal: a guardian holds
// still and charges its beam at a target it sees, then hurts it with magic
// and a hit.
type guardianAttackGoal struct {
	goalFlags
	attackTime int
}

func (g *guardianAttackGoal) CanUse(m *Mob) bool {
	target := m.Target()
	return target != nil && entityAlive(target)
}

func (g *guardianAttackGoal) CanContinueToUse(m *Mob) bool {
	if !g.CanUse(m) {
		return false
	}
	return guardianOf(m).kind == oceanElderGuardian || m.DistanceToSqr(m.Target()) > 9
}

func (g *guardianAttackGoal) Start(m *Mob) {
	g.attackTime = -10
	m.AI.nav.Stop()
	if target := m.Target(); target != nil {
		m.AI.look.SetLookAtEntityWith(target, 90, 90)
	}
}

func (g *guardianAttackGoal) Stop(m *Mob) {
	b := guardianOf(m)
	b.setBeamTarget(m, nil)
	m.SetTarget(nil)
	b.stroll.Trigger()
}

func (g *guardianAttackGoal) RequiresUpdateEveryTick() bool { return true }

func (g *guardianAttackGoal) Tick(m *Mob) {
	target := m.Target()
	if target == nil {
		return
	}
	b := guardianOf(m)
	m.AI.nav.Stop()
	m.AI.look.SetLookAtEntityWith(target, 90, 90)
	if !m.HasLineOfSight(target) {
		m.SetTarget(nil)
		return
	}
	g.attackTime++
	switch {
	case g.attackTime == 0:
		b.setBeamTarget(m, target.H())
		m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.guardian.attack", Volume: 1, Pitch: 1})
	case g.attackTime >= b.guardianAttackDuration():
		magic := 1.0
		if m.tx.World().Difficulty() == world.DifficultyHard {
			magic += 2
		}
		if b.kind == oceanElderGuardian {
			magic += 2
		}
		if l, ok := target.(Living); ok {
			l.Hurt(magic, IndirectMagicDamageSource{Projectile: m.Self, Owner: m.Self})
		}
		m.doHurtTarget(target)
		m.SetTarget(nil)
	}
}

// guardianMoveControl ports Guardian.GuardianMoveControl: a guardian swims
// towards the next node of its path with a wobble, looking ahead.
type guardianMoveControl struct{}

func (c *guardianMoveControl) tick(m *Mob) {
	b := guardianOf(m)
	mc := &m.AI.move
	if !mc.HasWanted() || m.AI.nav.IsDone() {
		m.SetSpeed(0)
		b.setMoving(m, false)
		return
	}
	wanted := mc.Wanted()
	delta := mgl64.Vec3{wanted[0] - m.data.Pos[0], wanted[1] - m.data.Pos[1], wanted[2] - m.data.Pos[2]}
	length := delta.Len()
	xd, yd, zd := delta[0]/length, delta[1]/length, delta[2]/length
	yaw := moveRotlerp(m.Yaw(), math.Atan2(delta[2], delta[0])*180/math.Pi-90, 90)
	m.SetYaw(yaw)
	m.AI.bodyYaw = yaw
	speed := m.AI.speed + (mc.SpeedModifier()*m.AI.MovementSpeed-m.AI.speed)*.125
	m.SetSpeed(speed)
	phase := float64(m.AI.tickCount + int(m.H().UniqueID()&0x7fffffff))
	push := math.Sin(phase*.5) * .05
	cos, sin := math.Cos(yaw*math.Pi/180), math.Sin(yaw*math.Pi/180)
	yPush := math.Sin(phase*.75) * .05
	m.data.Vel = m.data.Vel.Add(mgl64.Vec3{push * cos, yPush*(sin+cos)*.25 + speed*yd*.1, push * sin})
	look := &m.AI.look
	newLook := mgl64.Vec3{m.data.Pos[0] + xd*2, m.EyeY() + yd/length, m.data.Pos[2] + zd*2}
	oldLook := mgl64.Vec3(look.Wanted())
	if !look.LookingAtTarget() {
		oldLook = newLook
	}
	lerped := oldLook.Add(newLook.Sub(oldLook).Mul(.125))
	look.SetLookAtWith(lerped[0], lerped[1], lerped[2], 10, 40)
	b.setMoving(m, true)
}

// moveTowardsRestrictionGoal ports Java's MoveTowardsRestrictionGoal: a mob
// outside its home walks back towards it.
type moveTowardsRestrictionGoal struct {
	goalFlags
	speedModifier float64
	wanted        mgl64.Vec3
}

func (g *moveTowardsRestrictionGoal) CanUse(m *Mob) bool {
	home := m.AI.home
	if m.WithinHome(m.BlockPos()) {
		return false
	}
	pos, ok := DefaultRandomPosTowards(m, 16, 7, mgl64.Vec3{float64(home.pos[0]) + .5, float64(home.pos[1]), float64(home.pos[2]) + .5}, math.Pi/2)
	if ok {
		g.wanted = pos
	}
	return ok
}

func (g *moveTowardsRestrictionGoal) CanContinueToUse(m *Mob) bool { return !m.AI.nav.IsDone() }

func (g *moveTowardsRestrictionGoal) Start(m *Mob) {
	m.AI.nav.MoveToPos(m, g.wanted[0], g.wanted[1], g.wanted[2], g.speedModifier)
}
