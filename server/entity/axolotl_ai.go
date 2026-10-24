package entity

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports Axolotl and AxolotlAi: an axolotl walks and swims on its
// brain, hunts fish and fights drowned and guardians under water, and plays
// dead when hurt.

// axolotlMaxAir is Axolotl.AXOLOTL_TOTAL_AIR_SUPPLY.
const axolotlMaxAir = 6000

// tickAxolotl runs a tick of an axolotl: its air out of water, its ageing
// and love, and its MobAI with its brain.
func (b *AquaticMobBehaviour) tickAxolotl(e *Ent, tx *world.Tx) *Movement {
	a := &Axolotl{&AquaticMob{Ent: e}}
	ai := &b.ai
	// Axolotl.createAttributes and getMaxHeadXRot and getMaxHeadYRot.
	ai.MovementSpeed, ai.StepHeight = b.speed, 1
	ai.MaxHeadYaw, ai.MaxHeadPitch = 1, 1
	ai.EyeHeight = .2751
	if b.age < 0 {
		ai.EyeHeight = .09375
	}
	// Axolotl.handleAirSupply: an axolotl out of water and rain dries out
	// after five minutes.
	if !ai.NoAI {
		if pathWater(tx, cube.PosFromVec3(e.data.Pos)) || e.inRain(tx) {
			b.airTicks = 0
		} else if b.airTicks++; b.airTicks >= axolotlMaxAir+20 {
			b.airTicks = axolotlMaxAir
			a.Hurt(2, DryOutDamageSource{})
			if b.dead {
				return nil
			}
		}
	}
	tickAgeing(a, tx)
	return ai.Tick(e, a, b.registerAxolotl)
}

// registerAxolotl ports the navigation and controls of the constructor of
// Axolotl.
func (b *AquaticMobBehaviour) registerAxolotl(m *Mob) {
	m.AI.nav.SetAmphibious(true)
	m.AI.SetPathfindingMalus(PathWater, 0)
	m.AI.moveOverride = axolotlMoveControl{b: b, smooth: smoothSwimmingMoveControl{maxTurnX: 85, maxTurnY: 10, inWater: float64(float32(.1)), outsideWater: float64(float32(.5))}}
	m.AI.lookOverride = axolotlLookControl{b: b, smooth: smoothSwimmingLookControl{maxYawFromCentre: 20}}
}

// axolotlMoveControl ports Axolotl.AxolotlMoveControl: an axolotl playing
// dead does not move.
type axolotlMoveControl struct {
	b      *AquaticMobBehaviour
	smooth smoothSwimmingMoveControl
}

func (c axolotlMoveControl) tick(m *Mob) {
	if !c.b.playingDead {
		c.smooth.tick(m)
	}
}

// axolotlLookControl ports Axolotl.AxolotlLookControl: an axolotl playing
// dead does not look about.
type axolotlLookControl struct {
	b      *AquaticMobBehaviour
	smooth smoothSwimmingLookControl
}

func (c axolotlLookControl) tick(m *Mob) {
	if !c.b.playingDead {
		c.smooth.tick(m)
	}
}

// axolotlTravel ports Axolotl.travelInWater: an axolotl swims without
// sinking and keeps nine tenths of its speed.
func axolotlTravel(m *Mob, input mgl64.Vec3) bool {
	if !m.InWater() {
		return false
	}
	m.AI.moveRelative(m, m.AI.speed, input)
	m.AI.moveSelf(m, m.data.Vel)
	m.data.Vel = m.data.Vel.Mul(.9)
	return true
}

// axolotlServerAiStep ports Axolotl.customServerAiStep and
// AxolotlAi.updateActivity: a brain that stops fighting gives the axolotl
// two minutes before it hunts again.
func (b *AquaticMobBehaviour) axolotlServerAiStep(m *Mob) {
	br := b.br
	br.tick(m)
	old, _ := br.activeNonCore()
	if old != activityPlayDead {
		br.setActiveToFirstValid(activityPlayDead, activityFight, activityIdle)
		if now, _ := br.activeNonCore(); old == activityFight && now != activityFight {
			br.setWithExpiry(memHasHuntingCooldown, true, 2400)
		}
	}
	if !m.AI.NoAI {
		ticks, ok := br.int(memPlayDeadTicks)
		if playing := ok && ticks > 0; playing != b.playingDead {
			b.playingDead = playing
			m.updateState()
		}
	}
}

// axolotlSpeed ports AxolotlAi.getSpeedModifier.
func axolotlSpeed(inWater float64) func(m *Mob) float64 {
	return func(m *Mob) float64 {
		if m.InWater() {
			return inWater
		}
		return .15
	}
}

// newAxolotlBrain ports AxolotlAi and the brain provider of Axolotl.
func newAxolotlBrain() *Brain {
	b := newBrain(nearestLivingEntitySensor{}, adultSensor{}, hurtBySensor{}, axolotlAttackablesSensor{}, temptingSensor{items: animalTemptation})
	b.addActivity(activityCore, 0,
		newLookAtTargetSink(45, 90),
		newMoveToTargetSink(150, 250, nil),
		newValidatePlayDead(),
		newCountDownCooldownTicks(memTemptationCooldownTicks),
	)
	b.addActivityWith(activityIdle, []prioritizedBehavior{
		{0, newSetEntityLookTargetSometimes(isPlayerType, 6, uniformInt{30, 60})},
		{1, newAnimalMakeLove(AxolotlType, .2, 2)},
		{2, newRunOne(nil,
			weightedBehavior{newFollowTemptation(axolotlSpeed(.5), nil, false), 1},
			weightedBehavior{newBabyFollowAdult(uniformInt{5, 16}, axolotlSpeed(.6), memNearestVisibleAdult, false), 1},
		)},
		{3, newStartAttacking(nil, func(m *Mob) world.Entity {
			if isBreeding(m) {
				return nil
			}
			return m.Brain().entity(m, memNearestAttackable)
		})},
		{3, newTryFindLiquid(6, .15)},
		{4, &gateBehavior{entry: []memoryCondition{{memWalkTarget, memAbsent}}, children: []weightedBehavior{
			{newRandomSwim(.5), 2},
			{newRandomStroll(.15, false), 2},
			{newSetWalkTargetFromLookTarget(axolotlWalkToLookTarget, axolotlSpeed(.5), 3), 3},
			{newOneShotIf(func(m *Mob) bool { return m.InWater() }), 5},
			{newOneShotIf(func(m *Mob) bool { return m.OnGround() }), 5},
		}}},
	}, nil, nil)
	b.addActivityWith(activityFight, []prioritizedBehavior{
		{0, newStopAttackingIfTargetInvalid(nil, nil, true)},
		{1, newSetWalkTargetFromAttackTargetIfTargetOutOfReach(axolotlSpeed(.6))},
		{2, newMeleeAttack(20)},
		{3, newEraseMemoryIf(isBreeding, memAttackTarget)},
	}, []memoryCondition{{memAttackTarget, memPresent}}, []memoryType{memAttackTarget})
	b.addActivityWith(activityPlayDead, []prioritizedBehavior{
		{0, newPlayDead()},
		{1, newEraseMemoryIf(isBreeding, memPlayDeadTicks)},
	}, []memoryCondition{{memPlayDeadTicks, memPresent}}, []memoryType{memPlayDeadTicks})
	return b
}

// axolotlWalkToLookTarget ports AxolotlAi.canSetWalkTargetFromLookTarget: an
// axolotl walks to what it looks at only if that is in water as much as it
// is.
func axolotlWalkToLookTarget(m *Mob) bool {
	v, ok := m.Brain().get(memLookTarget)
	if !ok {
		return false
	}
	return pathWater(m.tx, v.(positionTracker).currentBlockPos(m.tx)) == m.InWater()
}

// newPlayDead ports PlayDead: an axolotl hurt in water stops and plays dead
// for up to ten seconds, regenerating.
func newPlayDead() *behavior {
	return &behavior{
		entry:       []memoryCondition{{memPlayDeadTicks, memPresent}, {memHurtByEntity, memPresent}},
		minDuration: 200, maxDuration: 200,
		checkExtra:  func(m *Mob) bool { return m.InWater() },
		canStillUse: func(m *Mob, _ int64) bool { return m.InWater() && m.Brain().has(memPlayDeadTicks) },
		start: func(m *Mob, _ int64) {
			b := m.Brain()
			b.erase(memWalkTarget)
			b.erase(memLookTarget)
			m.Self.(interface{ AddEffect(effect.Effect) }).AddEffect(effect.New(effect.Regeneration, 1, 10*time.Second))
		},
	}
}

// newValidatePlayDead ports ValidatePlayDead: the time an axolotl plays dead
// counts down, after which it forgets what hurt it.
func newValidatePlayDead() *oneShot {
	return newOneShot([]memoryCondition{{memPlayDeadTicks, memPresent}, {memHurtByEntity, memRegistered}}, func(m *Mob, b *Brain, _ int64) bool {
		ticks, _ := b.int(memPlayDeadTicks)
		if ticks <= 0 {
			b.erase(memPlayDeadTicks)
			b.erase(memHurtByEntity)
			b.useDefaultActivity()
		} else {
			b.set(memPlayDeadTicks, ticks-1)
		}
		return true
	})
}

// axolotlAttackablesSensor ports AxolotlAttackablesSensor: the nearest mob
// in water within 8 blocks an axolotl sees and may attack that is always
// hostile to it, or that it hunts while it has no hunting cooldown.
type axolotlAttackablesSensor struct{}

func (axolotlAttackablesSensor) scanRate() int { return 0 }
func (axolotlAttackablesSensor) requires() []memoryType {
	return []memoryType{memNearestAttackable, memNearestVisibleLivingEntities, memHasHuntingCooldown}
}

func (axolotlAttackablesSensor) doTick(m *Mob, b *Brain) {
	var nearest world.Entity
	if v := visibleLiving(m); v != nil {
		nearest = v.findClosest(m, func(e world.Entity) bool {
			if m.DistanceToSqr(e) > 64 || !entityInWater(m.tx, e) {
				return false
			}
			hostile := entityTypeInTag(e, "axolotl_always_hostiles")
			hunt := !b.has(memHasHuntingCooldown) && entityTypeInTag(e, "axolotl_hunt_targets")
			return (hostile || hunt) && sensorAttackable(m, e)
		})
	}
	b.setEntity(memNearestAttackable, nearest)
}

// axolotlSupportKiller ports Axolotl.onStopAttacking: when a mob an axolotl
// fights dies at the hands of a player within 20 blocks of it, the axolotl
// gives the player regeneration and takes their mining fatigue away. Java
// does so the next time the brain of the axolotl finds its target dead,
// here as the target dies.
func axolotlSupportKiller(tx *world.Tx, target world.Entity, src world.DamageSource) {
	killer := mobKiller(src)
	if killer == nil || !isPlayerEntity(killer) {
		return
	}
	for e := range tx.EntitiesWithin(cube.Box(-32, -32, -32, 32, 32, 32).Translate(target.Position())) {
		a, ok := e.(*Axolotl)
		if !ok {
			continue
		}
		b := a.behaviour()
		if b.br == nil {
			continue
		}
		if v, ok := b.br.get(memAttackTarget); !ok || v != target.H() {
			continue
		}
		box := e.H().Type().BBox(e).Translate(e.Position()).Grow(20)
		if killerBox := killer.H().Type().BBox(killer).Translate(killer.Position()); killerBox.IntersectsWith(box) {
			axolotlApplySupportingEffects(killer)
		}
	}
}

// axolotlApplySupportingEffects ports Axolotl.applySupportingEffects.
func axolotlApplySupportingEffects(player world.Entity) {
	p, ok := player.(interface {
		Effect(effect.Type) (effect.Effect, bool)
		AddEffect(effect.Effect)
		RemoveEffect(effect.Type)
	})
	if !ok {
		return
	}
	regen, has := p.Effect(effect.Regeneration)
	if !has || !regen.Infinite() && regen.Duration() <= 2399*time.Second/20 {
		previous := time.Duration(0)
		if has {
			previous = regen.Duration()
		}
		p.AddEffect(effect.New(effect.Regeneration, 1, min(2400*time.Second/20, 100*time.Second/20+previous)))
	}
	p.RemoveEffect(effect.MiningFatigue)
}

// doHurtTarget ports Mob.doHurtTarget for axolotls, which bite for their
// attack damage of 2.
func (b *AquaticMobBehaviour) doHurtTarget(m *Mob, target world.Entity) bool {
	if b.id != "minecraft:axolotl" {
		return false
	}
	l, ok := target.(Living)
	if !ok {
		return false
	}
	if _, hurt := l.Hurt(2, AttackDamageSource{Attacker: m.Self}); !hurt {
		return false
	}
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "mob.axolotl.attack", Volume: 1, Pitch: 1})
	return true
}

// PlayingDead reports if the axolotl plays dead, as Bedrock shows.
func (a *Axolotl) PlayingDead() bool { return a.behaviour().playingDead }
