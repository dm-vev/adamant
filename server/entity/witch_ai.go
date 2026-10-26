package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports the goals of Java's Witch.

func (b *WitchBehaviour) mobAI() *MobAI { return &b.ai }

func witchOf(m *Mob) *WitchBehaviour { return m.Behaviour().(*WitchBehaviour) }

// aiTick runs the MobAI of a witch and what Witch.aiStep does before it: a
// witch drinks potions that help it.
func (b *WitchBehaviour) aiTick(e *Ent, tx *world.Tx) *Movement {
	ai := &b.ai
	ai.MovementSpeed, ai.EyeHeight = b.speed, 1.62
	if b.drinking {
		// SPEED_MODIFIER_DRINKING of -0.25.
		ai.MovementSpeed = math.Max(0, b.speed-.25)
	}
	self := &Witch{Ent: e}
	if ai.started && !b.dead {
		b.tickPotions(ai.Mob(e, self))
	}
	return ai.Tick(e, self, b.registerGoals)
}

// registerGoals ports Witch.registerGoals.
func (b *WitchBehaviour) registerGoals(m *Mob) {
	ai := m.AI
	registerRaiderGoals(m)
	heal := &nearestHealableRaiderTargetGoal{NearestAttackableTargetGoal: NewNearestAttackableTypesGoal(raiderTypes, true)}
	heal.Selector = func(m *Mob, target world.Entity) bool {
		return witchOf(m).activeRaid(m) != nil && target.H().Type() != WitchType
	}
	b.heal = heal
	b.attack = NewNearestAttackableTargetGoal(nil, true)
	ai.Goals.Add(1, NewFloatGoal(m))
	ai.Goals.Add(2, NewRangedAttackGoal(1, 60, 10, b.performRangedAttack))
	ai.Goals.Add(2, NewWaterAvoidingRandomStrollGoal(1))
	ai.Goals.Add(3, NewLookAtPlayerGoal(8))
	ai.Goals.Add(3, NewRandomLookAroundGoal())
	hurt := NewHurtByTargetGoal()
	hurt.IgnoreDamageFrom = isRaiderEntity
	ai.Targets.Add(1, hurt)
	ai.Targets.Add(2, heal)
	ai.Targets.Add(3, &witchAttackTargetGoal{NearestAttackableTargetGoal: b.attack, witch: b})
}

// tickPotions ports the start of Witch.aiStep: while it heals raiders a
// witch attacks no players, and it drinks water breathing under water,
// fire resistance while burning, healing when hurt and swiftness to catch
// up with its target.
func (b *WitchBehaviour) tickPotions(m *Mob) {
	if b.heal != nil {
		b.heal.cooldown--
		b.attackAllow = b.heal.cooldown <= 0
	}
	if b.drinking {
		waited := b.potionUseTime
		b.potionUseTime--
		if waited > 0 {
			return
		}
		b.drinking = false
		for _, eff := range b.drinkPotion.Effects() {
			m.Self.(*Witch).AddEffect(eff)
		}
		m.tx.EmitGameEvent(world.GameEventDrink, m.data.Pos, world.GameEventContext{Source: m.Self})
		m.updateState()
		return
	}
	r := m.Random()
	var drink potion.Potion
	switch {
	case r.NextFloat() < .15 && drownedUnderWater(m) && !mobHasEffect(m, effect.WaterBreathing):
		drink = potion.WaterBreathing()
	case r.NextFloat() < .15 && (m.OnFireDuration() > 0 || m.tx.CurrentTick()-b.lastFireDamage <= 40) && !mobHasEffect(m, effect.FireResistance):
		drink = potion.FireResistance()
	case r.NextFloat() < .05 && b.health.Health() < b.health.MaxHealth():
		drink = potion.Healing()
	case r.NextFloat() < .5 && m.Target() != nil && !mobHasEffect(m, effect.Speed) && m.DistanceToSqr(m.Target()) > 121:
		drink = potion.Swiftness()
	default:
		if r.NextFloat() < 7.5e-4 {
			(&Witch{Ent: m.Ent}).viewAction(WitchMagicAction{})
		}
		return
	}
	b.drinking, b.potionUseTime, b.drinkPotion = true, 32, drink
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.witch.drink", Volume: 1, Pitch: .8 + float64(r.NextFloat())*.4})
	m.updateState()
	if r.NextFloat() < 7.5e-4 {
		(&Witch{Ent: m.Ent}).viewAction(WitchMagicAction{})
	}
}

// performRangedAttack ports Witch.performRangedAttack: a witch heals the
// raider it targets and throws slowness, poison, weakness or harming at
// others, aimed where the target heads.
func (b *WitchBehaviour) performRangedAttack(m *Mob, target world.Entity, _ float64) {
	if b.drinking {
		return
	}
	var vel mgl64.Vec3
	if v, ok := target.(interface{ Velocity() mgl64.Vec3 }); ok {
		vel = v.Velocity()
	}
	tp := target.Position()
	xd := tp[0] + vel[0] - m.data.Pos[0]
	yd := entityEyeY(target) - 1.1 - m.data.Pos[1]
	zd := tp[2] + vel[2] - m.data.Pos[2]
	dist := math.Sqrt(xd*xd + zd*zd)
	l, _ := target.(Living)
	has := func(t effect.Type) bool {
		if l == nil {
			return false
		}
		for _, e := range l.Effects() {
			if e.Type() == t {
				return true
			}
		}
		return false
	}
	p := potion.Harming()
	switch {
	case isRaiderEntity(target):
		if l != nil && l.Health() <= 4 {
			p = potion.Healing()
		} else {
			p = potion.Regeneration()
		}
		m.SetTarget(nil)
	case dist >= 8 && !has(effect.Slowness):
		p = potion.Slowness()
	case l != nil && l.Health() >= 8 && !has(effect.Poison):
		p = potion.Poison()
	case dist <= 3 && !has(effect.Weakness) && m.Random().NextFloat() < .25:
		p = potion.Weakness()
	}
	power := .75
	if dist <= 2 {
		power = .45
	}
	pos := mgl64.Vec3{m.data.Pos[0], m.EyeY() - .1, m.data.Pos[2]}
	// Witch.rangedAttackUncertainty is 8.
	velocity := shootDirection(m, mgl64.Vec3{xd, yd + dist*.2, zd}, power, 8)
	m.tx.AddEntity(NewSplashPotion(world.EntitySpawnOpts{Position: pos, Velocity: velocity}, p, m.Self))
	r := m.Random()
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.witch.throw", Volume: 1, Pitch: .8 + float64(r.NextFloat())*.4})
}

// witchMagicAbsorb ports Witch.getDamageAfterMagicAbsorb: a witch takes no
// damage from its own potions and little from magic and thorns.
func witchMagicAbsorb(w *Witch, d float64, src world.DamageSource) float64 {
	if a := damageAttacker(src); a != nil && a.H() == w.H() || splashOwner(src) == w.H() {
		return 0
	}
	switch src.(type) {
	case effect.InstantDamageSource, IndirectMagicDamageSource, enchantment.ThornsDamageSource:
		return d * .15
	}
	return d
}

// splashOwner returns the entity that threw the potion of a splash, if any.
func splashOwner(src world.DamageSource) *world.EntityHandle {
	if s, ok := src.(SplashPotionDamageSource); ok && s.Owner != nil {
		return s.Owner.H()
	}
	return nil
}

// nearestHealableRaiderTargetGoal ports Java's NearestHealableRaiderTargetGoal:
// in a raid, a witch now and then picks a raider to heal.
type nearestHealableRaiderTargetGoal struct {
	*NearestAttackableTargetGoal
	cooldown int
}

func (g *nearestHealableRaiderTargetGoal) CanUse(m *Mob) bool {
	if g.cooldown > 0 || !m.Random().NextBoolean() || witchOf(m).activeRaid(m) == nil {
		return false
	}
	g.RandomInterval = 0
	return g.NearestAttackableTargetGoal.CanUse(m)
}

func (g *nearestHealableRaiderTargetGoal) Start(m *Mob) {
	g.cooldown = reducedTickDelay(200)
	g.NearestAttackableTargetGoal.Start(m)
}

// witchAttackTargetGoal ports Java's NearestAttackableWitchTargetGoal: a
// witch hunts players while it heals no raiders.
type witchAttackTargetGoal struct {
	*NearestAttackableTargetGoal
	witch *WitchBehaviour
}

func (g *witchAttackTargetGoal) CanUse(m *Mob) bool {
	return g.witch.attackAllow && g.NearestAttackableTargetGoal.CanUse(m)
}
