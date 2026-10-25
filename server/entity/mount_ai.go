package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// This file ports the goals of Java's AbstractHorse, Llama and TraderLlama,
// the behaviours of CamelAi as goals, and the riding of AbstractHorse and
// Camel.

// registerGoals registers the goals of the species.
func (b *MountBehaviour) registerGoals(m *Mob) {
	switch {
	case b.camel():
		b.registerCamelGoals(m)
	case b.llama():
		b.registerLlamaGoals(m)
	default:
		b.registerHorseGoals(m)
	}
}

// registerHorseGoals ports AbstractHorse.registerGoals with the
// addBehaviourGoals of the horse species: skeleton horses neither float,
// panic nor follow food, and zombie horses do not panic.
func (b *MountBehaviour) registerHorseGoals(m *Mob) {
	ai := m.AI
	ai.Goals.Add(1, &runAroundLikeCrazyGoal{goalFlags: goalFlags{GoalMove}, mount: b})
	ai.Goals.Add(2, NewBreedGoal(1))
	ai.Goals.Add(4, NewFollowParentGoal(1))
	ai.Goals.Add(6, NewWaterAvoidingRandomStrollGoal(.7))
	ai.Goals.Add(7, NewLookAtPlayerGoal(6))
	ai.Goals.Add(8, NewRandomLookAroundGoal())
	if b.horseRearing() {
		ai.Goals.Add(9, &randomStandGoal{mount: b, nextStand: -400})
	}
	switch b.kind {
	case mountSkeletonHorse:
		ai.Goals.Add(1, &skeletonTrapGoal{mount: b})
	case mountZombieHorse:
		ai.Goals.Add(0, NewFloatGoal(m))
		ai.Goals.Add(3, NewTemptGoal(1.25, (&Mount{Ent: m.Ent}).breedingFood, false))
	default:
		ai.Goals.Add(0, NewFloatGoal(m))
		// AbstractHorse.MountPanicGoal: a horse a mob steers does not panic.
		b.panic = NewPanicGoal(1.2)
		b.panic.Causes = func(src world.DamageSource) bool { return !b.mobControlled() && panicCause(src) }
		ai.Goals.Add(1, b.panic)
		ai.Goals.Add(3, NewTemptGoal(1.25, func(s item.Stack) bool {
			return itemNamed(s, "minecraft:golden_carrot", "minecraft:golden_apple", "minecraft:enchanted_golden_apple")
		}, false))
	}
}

// registerLlamaGoals ports Llama.registerGoals and the goals TraderLlama
// adds: trader llamas panic faster and defend their trader from those who
// hurt it, zombies and illagers.
func (b *MountBehaviour) registerLlamaGoals(m *Mob) {
	ai := m.AI
	ai.Goals.Add(0, NewFloatGoal(m))
	ai.Goals.Add(1, &runAroundLikeCrazyGoal{goalFlags: goalFlags{GoalMove}, mount: b})
	ai.Goals.Add(2, &llamaFollowCaravanGoal{goalFlags: goalFlags{GoalMove}, mount: b, speed: 2.1})
	ai.Goals.Add(3, NewRangedAttackGoal(1.25, 40, 20, func(m *Mob, target world.Entity, _ float64) {
		(&Mount{Ent: m.Ent}).spit(m.tx, target)
	}))
	b.panic = NewPanicGoal(1.2)
	ai.Goals.Add(3, b.panic)
	ai.Goals.Add(4, NewBreedGoal(1))
	ai.Goals.Add(5, NewTemptGoal(1.25, func(s item.Stack) bool { return itemNamed(s, "minecraft:hay_block") }, false))
	ai.Goals.Add(6, NewFollowParentGoal(1))
	ai.Goals.Add(7, NewWaterAvoidingRandomStrollGoal(.7))
	ai.Goals.Add(8, NewLookAtPlayerGoal(6))
	ai.Goals.Add(9, NewRandomLookAroundGoal())
	ai.Targets.Add(1, llamaHurtByTargetGoal{NewHurtByTargetGoal(), b})
	// LlamaAttackWolfGoal spits at untamed wolves within a quarter of the
	// follow range it can reach.
	wolves := &NearestAttackableTargetGoal{targetGoal: newTargetGoal(false, true), RandomInterval: reducedTickDelay(16),
		IsType: func(e world.Entity) bool { return e.H().Type() == WolfType },
		Selector: func(_ *Mob, e world.Entity) bool {
			w, ok := e.(*Wolf)
			return ok && !w.Tamed()
		}}
	wolves.followScale = .25
	ai.Targets.Add(2, wolves)
	if b.kind != mountTraderLlama {
		return
	}
	b.panic = NewPanicGoal(2)
	ai.Goals.Add(1, b.panic)
	ai.Targets.Add(1, &traderLlamaDefendTraderGoal{targetGoal: newTargetGoal(false, false), stamp: -1})
	ai.Targets.Add(2, NewNearestAttackableTargetGoal(func(e world.Entity) bool {
		id := e.H().Type().EncodeEntity()
		return zombieClass[id] && id != "minecraft:zombified_piglin"
	}, true))
	ai.Targets.Add(2, NewNearestAttackableTargetGoal(func(e world.Entity) bool {
		switch e.H().Type().EncodeEntity() {
		case "minecraft:pillager", "minecraft:vindicator", "minecraft:evoker", "minecraft:illusioner":
			return true
		}
		return false
	}, true))
}

// registerCamelGoals ports the controls of the constructor of Camel, whose
// brain stands in for goals.
func (b *MountBehaviour) registerCamelGoals(m *Mob) {
	ai := m.AI
	ai.MaxHeadYaw = 30
	ai.nav.SetCanWalkOverFences(true)
	ai.moveOverride = camelMoveControl{b}
	ai.lookOverride = camelLookControl{b}
}

// playerController ports AbstractHorse.getControllingPassenger: a saddled
// mount is steered by the player riding it first.
func (b *MountBehaviour) playerController(tx *world.Tx) world.Entity {
	if !b.controlled() {
		return nil
	}
	e, ok := b.passengers[0].Entity(tx)
	if !ok {
		return nil
	}
	return e
}

// travel ports LivingEntity.travelRidden for mounts a player steers, with
// AbstractHorse.tickRidden, getRiddenInput and getRiddenSpeed, and
// Camel.travel, which keeps a sitting camel in place.
func (b *MountBehaviour) travel(m *Mob, input mgl64.Vec3) bool {
	refuse := b.refuseToMove(m.tx) && m.OnGround()
	if c := b.playerController(m.tx); c != nil && m.Alive() {
		b.tickRidden(m, c)
		strafe, forward := b.strafe*.5, b.forward
		if forward <= 0 {
			forward *= .25
		}
		if m.OnGround() && b.standCounter > 0 || b.refuseToMove(m.tx) {
			strafe, forward = 0, 0
		}
		if refuse {
			m.data.Vel[0], m.data.Vel[2] = 0, 0
		}
		m.AI.travelRidden(m, mgl64.Vec3{strafe, 0, forward}, b.riddenSpeed(c))
		return true
	}
	if !refuse {
		return false
	}
	m.data.Vel[0], m.data.Vel[2] = 0, 0
	input = mgl64.Vec3{0, input[1], 0}
	if m.InLiquid() {
		m.AI.travelInFluid(m, input)
	} else {
		m.AI.travelInAir(m, input)
	}
	return true
}

// tickRidden ports AbstractHorse.tickRidden and Camel.tickRidden: the mount
// turns the way its rider looks unless it is a camel that sits, and riding
// forward stands a sitting camel up.
func (b *MountBehaviour) tickRidden(m *Mob, rider world.Entity) {
	if b.camel() && b.forward > 0 && b.sitting && !b.inPoseTransition(m.tx) {
		b.standUp(&Mount{Ent: m.Ent})
	}
	if b.refuseToMove(m.tx) {
		return
	}
	rot := rider.Rotation()
	m.data.Rot = cube.Rotation{rot.Yaw(), rot.Pitch() * .5}
	m.AI.headYaw, m.AI.bodyYaw = rot.Yaw(), rot.Yaw()
}

// riddenSpeed ports getRiddenSpeed: the movement speed of the mount, and
// for a camel that may dash 0.1 more while its rider sprints.
func (b *MountBehaviour) riddenSpeed(rider world.Entity) float64 {
	speed := b.movementSpeed
	if b.camel() {
		if s, ok := rider.(interface{ Sprinting() bool }); ok && s.Sprinting() && b.dashCooldown == 0 {
			speed += .1
		}
	}
	return speed
}

// immobile ports AbstractHorse.isImmobile and Llama.isImmobile: a horse
// stands still while it eats or rears.
func (b *MountBehaviour) immobile() bool {
	if b.llama() {
		return b.eating
	}
	return b.eating || b.standCounter > 0
}

// waterSlowDown ports getWaterSlowDown: skeleton horses swim faster.
func (b *MountBehaviour) waterSlowDown() float64 {
	if b.kind == mountSkeletonHorse {
		return .96
	}
	return .8
}

// runAroundLikeCrazyGoal ports RunAroundLikeCrazyGoal: an untamed mount
// carrying a rider runs around and, every two and a half seconds on
// average, either accepts a player with a chance growing with its temper,
// or bucks its rider off and grows more tempered.
type runAroundLikeCrazyGoal struct {
	goalFlags
	mount *MountBehaviour
	pos   mgl64.Vec3
}

func (g *runAroundLikeCrazyGoal) CanUse(m *Mob) bool {
	b := g.mount
	if b.mobControlled() || b.tamed || !b.vehicle() {
		return false
	}
	pos, ok := DefaultRandomPos(m, 5, 4)
	g.pos = pos
	return ok
}

func (g *runAroundLikeCrazyGoal) Start(m *Mob) {
	m.AI.nav.MoveToPos(m, g.pos[0], g.pos[1], g.pos[2], 1.2)
}

func (g *runAroundLikeCrazyGoal) CanContinueToUse(m *Mob) bool {
	return !g.mount.tamed && !m.AI.nav.IsDone() && g.mount.vehicle()
}

func (g *runAroundLikeCrazyGoal) Tick(m *Mob) {
	b := g.mount
	if b.tamed || m.Random().NextInt(int32(adjustedTickDelay(g, 50))) != 0 {
		return
	}
	if b.passengers[0] == nil {
		return
	}
	mount := &Mount{Ent: m.Ent}
	if rider, ok := b.passengers[0].Entity(m.tx); ok && isPlayerEntity(rider) {
		if b.maxTemper() > 0 && int(m.Random().NextInt(int32(b.maxTemper()))) < b.temper {
			if p, ok := rider.(interface{ UUID() uuid.UUID }); ok {
				b.tameWithName(mount, p.UUID())
			}
			return
		}
		b.modifyTemper(5)
	}
	b.DismountAll(m.Ent, m.tx)
	b.makeMad(mount)
	mount.viewAction(TameAction{Succeeded: false})
}

// randomStandGoal ports RandomStandGoal: every 20 seconds and more often the
// longer it waited, a horse rears up one time in ten.
type randomStandGoal struct {
	goalFlags
	mount     *MountBehaviour
	nextStand int
}

func (g *randomStandGoal) CanUse(m *Mob) bool {
	g.nextStand++
	if g.nextStand <= 0 || int(m.Random().NextInt(1000)) >= g.nextStand {
		return false
	}
	g.nextStand = -400
	return !g.mount.immobile() && m.Random().NextInt(10) == 0
}

func (g *randomStandGoal) Start(m *Mob) {
	g.mount.standIfPossible(&Mount{Ent: m.Ent})
	m.tx.PlaySound(m.data.Pos, sound.NativeEvent{SoundType: "ambient", EntityType: m.H().Type().EncodeEntity(), ExtraData: -1})
}

func (g *randomStandGoal) CanContinueToUse(*Mob) bool { return false }

func (g *randomStandGoal) RequiresUpdateEveryTick() bool { return true }

// llamaFollowCaravanGoal ports LlamaFollowCaravanGoal: a free llama joins
// the end of a caravan within nine blocks, or starts one behind a leashed
// llama, as long as a leashed llama leads it, and walks up to two blocks
// behind the llama ahead of it.
type llamaFollowCaravanGoal struct {
	goalFlags
	mount     *MountBehaviour
	speed     float64
	distCheck int
}

func (g *llamaFollowCaravanGoal) CanUse(m *Mob) bool {
	b := g.mount
	self := &Mount{Ent: m.Ent}
	if self.leashed() || b.llamaState.caravanHead != nil {
		return false
	}
	var closest *Mount
	best := math.MaxFloat64
	box := m.BBox().GrowVec3(mgl64.Vec3{9, 4, 9})
	for _, pass := range [2]func(*Mount) bool{
		func(o *Mount) bool { return o.b().llamaState.caravanHead != nil },
		func(o *Mount) bool { return o.leashed() },
	} {
		for e := range m.tx.EntitiesWithin(box) {
			o := mountFromEntity(e)
			if o == nil || e.H() == m.H() || !o.b().llama() || o.b().llamaState.caravanTail != nil || !pass(o) {
				continue
			}
			if d := m.DistanceToSqr(e); d <= best {
				closest, best = o, d
			}
		}
		if closest != nil {
			break
		}
	}
	if closest == nil || best < 4 || !closest.leashed() && !caravanLeashed(m.tx, closest, 1) {
		return false
	}
	b.llamaState.caravanHead = closest.H()
	closest.b().llamaState.caravanTail = m.H()
	return true
}

func (g *llamaFollowCaravanGoal) CanContinueToUse(m *Mob) bool {
	b := g.mount
	head := llamaMount(m.tx, b.llamaState.caravanHead)
	if head == nil || !caravanLeashed(m.tx, &Mount{Ent: m.Ent}, 0) {
		return false
	}
	if m.DistanceToSqr(head) > 676 {
		if g.speed <= 3 {
			g.speed *= 1.2
			g.distCheck = reducedTickDelay(40)
			return true
		}
		if g.distCheck == 0 {
			return false
		}
	}
	if g.distCheck > 0 {
		g.distCheck--
	}
	return true
}

func (g *llamaFollowCaravanGoal) Stop(m *Mob) {
	g.mount.leaveCaravan(m.tx)
	g.speed = 2.1
}

func (g *llamaFollowCaravanGoal) Tick(m *Mob) {
	head := llamaMount(m.tx, g.mount.llamaState.caravanHead)
	if head == nil || (&Mount{Ent: m.Ent}).leashedToKnot(m.tx) {
		return
	}
	d := head.Position().Sub(m.data.Pos)
	if d.Len() == 0 {
		return
	}
	to := m.data.Pos.Add(d.Normalize().Mul(math.Max(d.Len()-2, 0)))
	m.AI.nav.MoveToPos(m, to[0], to[1], to[2], g.speed)
}

// llamaHurtByTargetGoal ports Llama.LlamaHurtByTargetGoal: a llama spits at
// whoever hurt it once and leaves it be.
type llamaHurtByTargetGoal struct {
	*HurtByTargetGoal
	mount *MountBehaviour
}

func (g llamaHurtByTargetGoal) CanContinueToUse(m *Mob) bool {
	if g.mount.llamaState.didSpit {
		g.mount.llamaState.didSpit = false
		return false
	}
	return g.HurtByTargetGoal.CanContinueToUse(m)
}

// traderLlamaDefendTraderGoal ports
// TraderLlama.TraderLlamaDefendWanderingTraderGoal: a trader llama targets
// whoever hurt the wandering trader holding its lead.
type traderLlamaDefendTraderGoal struct {
	targetGoal
	attacker *world.EntityHandle
	stamp    int64
}

func (g *traderLlamaDefendTraderGoal) CanUse(m *Mob) bool {
	trader, ok := leashedTrader(m.Ent, m.tx)
	if !ok {
		return false
	}
	attacker, stamp, ok := trader.lastAttacker(m.tx)
	if !ok || stamp == g.stamp || !g.canAttack(m, attacker, forCombat()) {
		return false
	}
	g.attacker = attacker.H()
	return true
}

func (g *traderLlamaDefendTraderGoal) Start(m *Mob) {
	if attacker, ok := g.attacker.Entity(m.tx); ok {
		m.SetTarget(attacker)
	}
	if trader, ok := leashedTrader(m.Ent, m.tx); ok {
		if _, stamp, ok := trader.lastAttacker(m.tx); ok {
			g.stamp = stamp
		}
	}
	g.start()
}

func (g *traderLlamaDefendTraderGoal) CanContinueToUse(m *Mob) bool { return g.canContinue(m) }

func (g *traderLlamaDefendTraderGoal) Stop(m *Mob) { g.stop(m) }

// camelMoveControl ports CamelMoveControl: a sitting camel off its lead
// that wants to walk somewhere stands up first.
type camelMoveControl struct{ mount *MountBehaviour }

func (c camelMoveControl) tick(m *Mob) {
	b := c.mount
	if m.AI.move.HasWanted() && m.Ent.HolderLeashID() == 0 && b.sitting && !b.inPoseTransition(m.tx) {
		b.standUp(&Mount{Ent: m.Ent})
	}
	m.AI.move.tick(m)
}

// mountJumpPower ports LivingEntity.getJumpPower for the jump of a mount at
// the charge passed.
func mountJumpPower(m *Mob, strength, amount float64) float64 {
	return float64(float32(strength)*float32(amount)*float32(blockJumpFactor(m))) + jumpBoostPower(m)
}

// yawSinCos returns Mth.sin and Mth.cos of a yaw in degrees.
func yawSinCos(yaw float64) (float64, float64) {
	r := float64(float32(yaw) * float32(math.Pi/180))
	return float64(vanilla.Sin(r)), float64(vanilla.Cos(r))
}
