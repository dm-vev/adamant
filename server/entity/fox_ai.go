package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports the goals and controls of Java's Fox.

func foxOf(m *Mob) *foxState { return m.Behaviour().(*foxState) }

// trusts ports Fox.trusts.
func (s *foxState) trusts(e world.Entity) bool {
	id := e.H().UUID().String()
	for _, t := range s.trusted {
		if t == id {
			return true
		}
	}
	return false
}

// canMove ports Fox.canMove.
func (s *foxState) canMove() bool { return !s.sleeping && !s.sitting && !s.faceplanted }

// clearStates ports Fox.clearStates.
func (s *foxState) clearStates() {
	s.interested, s.crouching, s.sitting, s.sleeping, s.defending, s.faceplanted = false, false, false, false, false, false
}

// registerGoals ports Fox.registerGoals and Fox.setTargetGoals, which rank
// the hunt of land prey first for red foxes and of fish first for snow
// foxes.
func (s *foxState) registerGoals(m *Mob) {
	ai := m.AI
	ai.FollowRange = 32
	ai.SetPathfindingMalus(PathDamagingInNeighbour, 0)
	ai.SetPathfindingMalus(PathDamaging, 0)
	ai.nav.SetRequiredPathLength(m, 32)
	ai.moveOverride = foxMoveControl{s}
	ai.lookOverride = foxLookControl{s}

	ai.Goals.Add(0, foxFloatGoal{NewFloatGoal(m), s})
	ai.Goals.Add(0, newClimbOnTopOfPowderSnowGoal())
	ai.Goals.Add(1, &foxFaceplantGoal{goalFlags: goalFlags{GoalLook | GoalJump | GoalMove}, fox: s})
	s.panic = NewPanicGoal(2.2)
	s.panic.Causes = func(src world.DamageSource) bool { return !s.defending && panicCause(src) }
	ai.Goals.Add(2, s.panic)
	ai.Goals.Add(3, foxBreedGoal{NewBreedGoal(1)})
	players := NewAvoidEntityTypesGoal(playerTypes, 16, 1.6, 1.4)
	players.Predicate = func(e world.Entity) bool { return !discrete(e) && !s.trusts(e) && !s.defending }
	ai.Goals.Add(4, players)
	wolves := NewAvoidEntityGoal(func(e world.Entity) bool { return e.H().Type() == WolfType }, 8, 1.6, 1.4)
	wolves.Predicate = func(e world.Entity) bool { return !e.(*Wolf).Tamed() && !s.defending }
	ai.Goals.Add(4, wolves)
	bears := NewAvoidEntityGoal(func(e world.Entity) bool { return e.H().Type() == PolarBearType }, 8, 1.6, 1.4)
	bears.Predicate = func(world.Entity) bool { return !s.defending }
	ai.Goals.Add(4, bears)
	ai.Goals.Add(5, &foxStalkPreyGoal{goalFlags: goalFlags{GoalMove | GoalLook}, fox: s})
	ai.Goals.Add(6, &foxPounceGoal{goalFlags: goalFlags{GoalMove | GoalJump}, fox: s})
	ai.Goals.Add(6, &foxSeekShelterGoal{FleeSunGoal: NewFleeSunGoal(1.25), fox: s, interval: reducedTickDelay(100)})
	melee := NewMeleeAttackGoal(1.2, true)
	melee.CheckAttack = func(m *Mob, g *MeleeAttackGoal, target world.Entity) {
		if g.CanPerformAttack(m, target) {
			g.ResetAttackCooldown()
			m.doHurtTarget(target)
			m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.fox.bite", Volume: 1, Pitch: 1})
		}
	}
	ai.Goals.Add(7, foxMeleeAttackGoal{melee, s})
	ai.Goals.Add(7, &foxSleepGoal{goalFlags: goalFlags{GoalMove | GoalLook | GoalJump}, fox: s, countdown: int(m.Random().NextInt(int32(reducedTickDelay(140))))})
	ai.Goals.Add(8, foxFollowParentGoal{NewFollowParentGoal(1.25), s})
	ai.Goals.Add(9, &foxStrollThroughVillageGoal{goalFlags: goalFlags{GoalMove}, fox: s, interval: reducedTickDelay(200)})
	ai.Goals.Add(10, &foxEatBerriesGoal{moveToBlock: moveToBlock{acceptedDistance: 2, recalcInterval: 100}, fox: s})
	ai.Goals.Add(10, newLeapAtTargetGoal(.4))
	ai.Goals.Add(11, NewWaterAvoidingRandomStrollGoal(1))
	ai.Goals.Add(11, &foxSearchForItemsGoal{goalFlags: goalFlags{GoalMove}, fox: s})
	ai.Goals.Add(12, foxLookAtPlayerGoal{NewLookAtPlayerGoal(24), s})
	ai.Goals.Add(13, &foxPerchAndSearchGoal{goalFlags: goalFlags{GoalMove | GoalLook}, fox: s})

	defend := &NearestAttackableTargetGoal{targetGoal: newTargetGoal(false, false), RandomInterval: reducedTickDelay(10),
		IsType: func(e world.Entity) bool { _, ok := e.(Living); return ok }}
	defend.Selector = func(m *Mob, target world.Entity) bool {
		victim, _ := lastHurtMobOf(target)
		return victim != nil && !s.trusts(target)
	}
	ai.Targets.Add(3, &foxDefendTrustedTargetGoal{NearestAttackableTargetGoal: defend, fox: s, timestamp: -1})
	land := NewNearestAttackableTargetGoal(func(e world.Entity) bool {
		return e.H().Type() == ChickenType || e.H().Type() == RabbitType
	}, false)
	turtles := NewNearestAttackableTypesGoal(entityTypes{"minecraft:turtle"}, false)
	turtles.Selector = turtleBabyOnLand
	fish := NewNearestAttackableTargetGoal(func(e world.Entity) bool {
		switch e.H().Type().EncodeEntity() {
		case "minecraft:cod", "minecraft:salmon", "minecraft:tropical_fish":
			return true
		}
		return false
	}, false)
	fish.RandomInterval = reducedTickDelay(20)
	if s.variant == 0 {
		ai.Targets.Add(4, land)
		ai.Targets.Add(4, turtles)
		ai.Targets.Add(6, fish)
	} else {
		ai.Targets.Add(4, fish)
		ai.Targets.Add(6, land)
		ai.Targets.Add(6, turtles)
	}
}

// lastHurtMobOf returns the living entity an entity last hurt and when.
func lastHurtMobOf(e world.Entity) (world.Entity, int64) {
	return ownerLastHurtMob(e)
}

// discrete ports Entity.isDiscrete: a sneaking player.
func discrete(e world.Entity) bool {
	s, ok := e.(interface{ Sneaking() bool })
	return ok && s.Sneaking()
}

// targetSet ports Fox.setTarget: a fox that loses its target stops
// defending.
func (s *foxState) targetSet(_ *Mob, target world.Entity) {
	if target == nil && s.defending {
		s.defending = false
	}
}

// doHurtTarget ports Mob.doHurtTarget for foxes, which bite for 2 damage.
func (s *foxState) doHurtTarget(m *Mob, target world.Entity) bool {
	l, ok := target.(Living)
	if !ok {
		return false
	}
	if _, hurt := l.Hurt(2, AttackDamageSource{Attacker: m.Self}); !hurt {
		return false
	}
	l.KnockBack(m.data.Pos, .4, .4)
	return true
}

// alertable ports FoxBehaviorGoal.alertable: something within twelve
// blocks a fox keeps an eye on, prey, monsters, untamed pets or players it
// does not trust that are awake and not sneaking.
func (s *foxState) alertable(m *Mob) bool {
	c := forCombat()
	c.rangeLimit, c.ignoreSight = 12, true
	for e := range m.tx.EntitiesWithin(m.BBox().GrowVec3(mgl64.Vec3{12, 6, 12})) {
		if e.H() == m.H() || !c.test(m, e) || !s.alertableEntity(e) {
			continue
		}
		return true
	}
	return false
}

// alertableEntity ports Fox.FoxAlertableEntitiesSelector.
func (s *foxState) alertableEntity(e world.Entity) bool {
	switch {
	case e.H().Type() == FoxType:
		return false
	case e.H().Type() == ChickenType || e.H().Type() == RabbitType || monsterClass[e.H().Type().EncodeEntity()]:
		return true
	}
	if t, ok := e.(interface{ Tamed() bool }); ok {
		return !t.Tamed()
	}
	if isPlayerEntity(e) && creativeOrSpectator(e) {
		return false
	}
	if s.trusts(e) {
		return false
	}
	if sl, ok := e.(interface{ Sleeping() (cube.Pos, bool) }); ok {
		if _, sleeping := sl.Sleeping(); sleeping {
			return false
		}
	}
	return !discrete(e)
}

// hasShelter ports FoxBehaviorGoal.hasShelter: the fox cannot see the sky
// and likes where it stands.
func (s *foxState) hasShelter(m *Mob) bool {
	pos := cube.PosFromVec3(mgl64.Vec3{m.data.Pos[0], m.BBox().Max()[1], m.data.Pos[2]})
	return !canSeeSky(m.tx, pos) && m.walkTargetValue(pos) >= 0
}

// pathClear ports Fox.isPathClear: nothing but replaceable blocks lies in
// the three blocks above the line from the fox to its target.
func pathClear(m *Mob, target world.Entity) bool {
	tp := target.Position()
	zdiff, xdiff := tp[2]-m.data.Pos[2], tp[0]-m.data.Pos[0]
	slope := zdiff / xdiff
	for i := range 6 {
		z := 0.0
		if slope != 0 {
			z = zdiff * float64(float32(i)/6)
		}
		x := xdiff * float64(float32(i)/6)
		if slope != 0 {
			x = z / slope
		}
		for j := 1; j < 4; j++ {
			pos := cube.PosFromVec3(mgl64.Vec3{m.data.Pos[0] + x, m.data.Pos[1] + float64(j), m.data.Pos[2] + z})
			if r, ok := m.tx.Block(pos).(block.Replaceable); !ok || !r.ReplaceableBy(block.Stone{}) {
				return false
			}
		}
	}
	return true
}

// foxMoveControl ports FoxMoveControl: a sleeping, sitting or faceplanted
// fox does not walk.
type foxMoveControl struct{ fox *foxState }

func (c foxMoveControl) tick(m *Mob) {
	if c.fox.canMove() {
		m.AI.move.tick(m)
	}
}

// foxLookControl ports FoxLookControl: a sleeping fox keeps its head still.
type foxLookControl struct{ fox *foxState }

func (c foxLookControl) tick(m *Mob) {
	if !c.fox.sleeping {
		m.AI.look.tick(m)
	}
}

// foxFloatGoal ports FoxFloatGoal: a fox swims in fluid deeper than a
// quarter of a block and forgets what it did.
type foxFloatGoal struct {
	*FloatGoal
	fox *foxState
}

func (g foxFloatGoal) CanUse(m *Mob) bool { return m.AI.waterHeight > .25 || m.InLava() }

func (g foxFloatGoal) Start(*Mob) { g.fox.clearStates() }

// foxFaceplantGoal ports FaceplantGoal: a fox that dove into snow stays
// stuck for a second.
type foxFaceplantGoal struct {
	goalFlags
	fox       *foxState
	countdown int
}

func (g *foxFaceplantGoal) CanUse(*Mob) bool { return g.fox.faceplanted }

func (g *foxFaceplantGoal) CanContinueToUse(m *Mob) bool { return g.CanUse(m) && g.countdown > 0 }

func (g *foxFaceplantGoal) Start(*Mob) { g.countdown = adjustedTickDelay(g, 40) }

func (g *foxFaceplantGoal) Stop(*Mob) { g.fox.faceplanted = false }

func (g *foxFaceplantGoal) Tick(*Mob) { g.countdown-- }

// foxBreedGoal ports FoxBreedGoal, which clears the states of both foxes.
type foxBreedGoal struct{ *BreedGoal }

func (g foxBreedGoal) Start(m *Mob) {
	foxOf(m).clearStates()
	if p, ok := g.partnerOf(m).(*Fox); ok {
		p.b().clearStates()
	}
}

// foxStalkPreyGoal ports StalkPreyGoal: a fox creeps up on a chicken or
// rabbit and crouches six blocks away, ready to pounce.
type foxStalkPreyGoal struct {
	goalFlags
	fox *foxState
}

func (g *foxStalkPreyGoal) CanUse(m *Mob) bool {
	s := g.fox
	if s.sleeping {
		return false
	}
	target := m.Target()
	return target != nil && entityAlive(target) && (target.H().Type() == ChickenType || target.H().Type() == RabbitType) &&
		m.DistanceToSqr(target) > 36 && !s.crouching && !s.interested && !m.AI.jumping
}

func (g *foxStalkPreyGoal) Start(*Mob) { g.fox.sitting, g.fox.faceplanted = false, false }

func (g *foxStalkPreyGoal) Stop(m *Mob) {
	s := g.fox
	if target := m.Target(); target != nil && pathClear(m, target) {
		s.interested, s.crouching = true, true
		m.AI.nav.Stop()
		m.AI.look.SetLookAtEntityWith(target, m.AI.MaxHeadYaw, m.AI.MaxHeadPitch)
		return
	}
	s.interested, s.crouching = false, false
}

func (g *foxStalkPreyGoal) Tick(m *Mob) {
	target := m.Target()
	if target == nil {
		return
	}
	m.AI.look.SetLookAtEntityWith(target, m.AI.MaxHeadYaw, m.AI.MaxHeadPitch)
	if m.DistanceToSqr(target) <= 36 {
		g.fox.interested, g.fox.crouching = true, true
		m.AI.nav.Stop()
	} else {
		m.AI.nav.MoveToEntity(m, target, 1.5)
	}
}

// foxPounceGoal ports FoxPounceGoal: a fully crouched fox with a clear path
// leaps at its prey, biting it on landing, and dives head first into snow.
type foxPounceGoal struct {
	goalFlags
	fox *foxState
}

func (g *foxPounceGoal) CanUse(m *Mob) bool {
	s := g.fox
	if s.crouchAmount != 5 {
		return false
	}
	target := m.Target()
	if target == nil || !entityAlive(target) {
		return false
	}
	if !pathClear(m, target) {
		m.AI.nav.CreatePathToEntity(m, target, 0)
		s.crouching, s.interested = false, false
		return false
	}
	return true
}

func (g *foxPounceGoal) CanContinueToUse(m *Mob) bool {
	target := m.Target()
	if target == nil || !entityAlive(target) {
		return false
	}
	yd := m.data.Vel[1]
	return (yd*yd >= .05 || math.Abs(m.data.Rot.Pitch()) >= 15 || !m.OnGround()) && !g.fox.faceplanted
}

func (g *foxPounceGoal) IsInterruptable() bool { return false }

func (g *foxPounceGoal) Start(m *Mob) {
	s := g.fox
	m.AI.jumping = true
	s.pouncing, s.interested = true, false
	if target := m.Target(); target != nil {
		m.AI.look.SetLookAtEntityWith(target, 60, 30)
		uv := target.Position().Sub(m.data.Pos)
		if uv.Len() > 0 {
			uv = uv.Normalize()
		}
		m.data.Vel = m.data.Vel.Add(mgl64.Vec3{uv[0] * .8, .9, uv[2] * .8})
	}
	m.AI.nav.Stop()
}

func (g *foxPounceGoal) Stop(*Mob) {
	s := g.fox
	s.crouching, s.crouchAmount, s.interested, s.pouncing = false, 0, false, false
}

func (g *foxPounceGoal) Tick(m *Mob) {
	s := g.fox
	target := m.Target()
	if target != nil {
		m.AI.look.SetLookAtEntityWith(target, 60, 30)
	}
	if !s.faceplanted {
		v := m.data.Vel
		if v[1]*v[1] < .03 && m.data.Rot.Pitch() != 0 {
			m.data.Rot[1] = rotlerpDelta(.2, m.data.Rot.Pitch(), 0)
		} else {
			direction := math.Hypot(v[0], v[2])
			bias := 1.0
			if m.AI.jumping && v[1] > 0 {
				bias = 6.5
			}
			biasedY := v[1] * bias
			if l := math.Hypot(direction, biasedY); l > 1e-5 {
				sign := 1.0
				if -biasedY < 0 {
					sign = -1
				} else if biasedY == 0 {
					sign = 0
				}
				m.data.Rot[1] = sign * math.Acos(direction/l) * 180 / math.Pi
			}
		}
	}
	if target != nil && math.Sqrt(m.DistanceToSqr(target)) <= 2 {
		m.doHurtTarget(target)
	} else if m.data.Rot.Pitch() > 0 && m.OnGround() && float32(m.data.Vel[1]) != 0 {
		if _, snow := m.tx.Block(m.BlockPos()).(block.SnowLayer); snow {
			m.data.Rot[1] = 60
			m.SetTarget(nil)
			s.faceplanted = true
		}
	}
}

// rotlerpDelta ports Mth.rotLerp: a fraction of the way from a to b.
func rotlerpDelta(f, a, b float64) float64 {
	d := math.Mod(b-a+540, 360) - 180
	return a + f*d
}

// foxSeekShelterGoal ports SeekShelterGoal: out of a village in daylight or
// a thunderstorm, a fox looks for cover now and then.
type foxSeekShelterGoal struct {
	*FleeSunGoal
	fox      *foxState
	interval int
}

func (g *foxSeekShelterGoal) CanUse(m *Mob) bool {
	if g.fox.sleeping || m.Target() != nil {
		return false
	}
	pos := m.BlockPos()
	if m.tx.ThunderingAt(pos) && canSeeSky(m.tx, pos) {
		return g.setWantedPos(m)
	}
	if g.interval > 0 {
		g.interval--
		return false
	}
	g.interval = 100
	return brightOutside(m.tx) && canSeeSky(m.tx, pos) && !m.tx.IsVillage(pos) && g.setWantedPos(m)
}

func (g *foxSeekShelterGoal) setWantedPos(m *Mob) bool {
	pos, ok := g.hidePos(m)
	if ok {
		g.wanted = pos
	}
	return ok
}

func (g *foxSeekShelterGoal) Start(m *Mob) {
	g.fox.clearStates()
	g.FleeSunGoal.Start(m)
}

// foxMeleeAttackGoal ports FoxMeleeAttackGoal: a fox only bites standing
// up.
type foxMeleeAttackGoal struct {
	*MeleeAttackGoal
	fox *foxState
}

func (g foxMeleeAttackGoal) CanUse(m *Mob) bool {
	s := g.fox
	return !s.sitting && !s.sleeping && !s.crouching && !s.faceplanted && g.MeleeAttackGoal.CanUse(m)
}

func (g foxMeleeAttackGoal) Start(m *Mob) {
	g.fox.interested = false
	g.MeleeAttackGoal.Start(m)
}

// foxSleepGoal ports SleepGoal: by day a still fox under cover with nothing
// around to watch lies down to sleep.
type foxSleepGoal struct {
	goalFlags
	fox       *foxState
	countdown int
}

func (g *foxSleepGoal) CanUse(m *Mob) bool {
	if m.AI.xxa != 0 || m.AI.yya != 0 || m.AI.zza != 0 {
		return false
	}
	return g.canSleep(m) || g.fox.sleeping
}

func (g *foxSleepGoal) CanContinueToUse(m *Mob) bool { return g.canSleep(m) }

func (g *foxSleepGoal) canSleep(m *Mob) bool {
	if g.countdown > 0 {
		g.countdown--
		return false
	}
	return brightOutside(m.tx) && g.fox.hasShelter(m) && !g.fox.alertable(m) && !m.data.InPowderSnow
}

func (g *foxSleepGoal) Stop(m *Mob) {
	g.countdown = int(m.Random().NextInt(int32(reducedTickDelay(140))))
	g.fox.clearStates()
}

func (g *foxSleepGoal) Start(m *Mob) {
	s := g.fox
	s.sitting, s.crouching, s.interested = false, false, false
	m.AI.jumping = false
	s.sleeping = true
	m.AI.nav.Stop()
	m.AI.move.SetWantedPosition(m.data.Pos[0], m.data.Pos[1], m.data.Pos[2], 0)
}

// foxFollowParentGoal ports FoxFollowParentGoal, which a defending fox does
// not start.
type foxFollowParentGoal struct {
	*FollowParentGoal
	fox *foxState
}

func (g foxFollowParentGoal) CanUse(m *Mob) bool {
	return !g.fox.defending && g.FollowParentGoal.CanUse(m)
}

func (g foxFollowParentGoal) CanContinueToUse(m *Mob) bool {
	return !g.fox.defending && g.FollowParentGoal.CanContinueToUse(m)
}

func (g foxFollowParentGoal) Start(m *Mob) {
	g.fox.clearStates()
	g.FollowParentGoal.Start(m)
}

// foxStrollThroughVillageGoal ports FoxStrollThroughVillageGoal, a
// StrollThroughVillageGoal: at night a fox near a village strolls towards
// its heart.
type foxStrollThroughVillageGoal struct {
	goalFlags
	fox      *foxState
	interval int
	wanted   cube.Pos
	hasWant  bool
}

func (g *foxStrollThroughVillageGoal) canFoxMove(m *Mob) bool {
	s := g.fox
	return !s.sleeping && !s.sitting && !s.defending && m.Target() == nil
}

func (g *foxStrollThroughVillageGoal) CanUse(m *Mob) bool {
	if brightOutside(m.tx) || m.Random().NextInt(int32(g.interval)) != 0 || !m.tx.IsVillage(m.BlockPos()) {
		return false
	}
	pos, ok := LandRandomPosWeighted(m, 15, 7, func(p cube.Pos) float64 { return -float64(m.tx.VillageDistance(p)) })
	g.hasWant = ok
	if ok {
		g.wanted = cube.PosFromVec3(pos)
	}
	return ok && g.canFoxMove(m)
}

func (g *foxStrollThroughVillageGoal) CanContinueToUse(m *Mob) bool {
	return g.hasWant && !m.AI.nav.IsDone() && m.AI.nav.targetPos != nil && *m.AI.nav.targetPos == g.wanted && g.canFoxMove(m)
}

func (g *foxStrollThroughVillageGoal) Start(*Mob) { g.fox.clearStates() }

func (g *foxStrollThroughVillageGoal) Tick(m *Mob) {
	if !g.hasWant || !m.AI.nav.IsDone() || g.wanted.Vec3Centre().Sub(m.data.Pos).LenSqr() < 100 {
		return
	}
	wanted := mgl64.Vec3{float64(g.wanted[0]) + .5, float64(g.wanted[1]), float64(g.wanted[2]) + .5}
	wanted = m.data.Pos.Sub(wanted).Mul(.4).Add(wanted)
	to := wanted.Sub(m.data.Pos)
	if to.Len() > 0 {
		to = to.Normalize()
	}
	to = to.Mul(10).Add(m.data.Pos)
	p := cube.PosFromVec3(to)
	p[1] = m.tx.HighestBlock(p[0], p[2]) + 1
	if !m.AI.nav.MoveToPos(m, float64(p[0]), float64(p[1]), float64(p[2]), 1) {
		r := m.Random()
		q := m.BlockPos().Add(cube.Pos{-8 + int(r.NextInt(16)), 0, -8 + int(r.NextInt(16))})
		q[1] = m.tx.HighestBlock(q[0], q[2]) + 1
		m.AI.nav.MoveToPos(m, float64(q[0]), float64(q[1]), float64(q[2]), 1)
	}
}

// foxEatBerriesGoal ports FoxEatBerriesGoal: a fox walks to a sweet berry
// bush with berries or glow berries within twelve blocks, sniffing, waits
// two seconds and picks them.
type foxEatBerriesGoal struct {
	moveToBlock
	fox         *foxState
	ticksWaited int
}

func (g *foxEatBerriesGoal) Flags() GoalFlag { return GoalMove | GoalJump }

func (g *foxEatBerriesGoal) valid(m *Mob, pos cube.Pos) bool {
	switch b := m.tx.Block(pos).(type) {
	case block.SweetBerries:
		return b.Growth >= 2
	case block.CaveVines:
		return b.HasBerries()
	}
	return false
}

func (g *foxEatBerriesGoal) CanUse(m *Mob) bool {
	return !g.fox.sleeping && g.moveToBlock.canUse(m, 12, 1, g.valid)
}

func (g *foxEatBerriesGoal) CanContinueToUse(m *Mob) bool {
	return g.moveToBlock.canContinueToUse(m, g.valid)
}

func (g *foxEatBerriesGoal) RequiresUpdateEveryTick() bool { return true }

func (g *foxEatBerriesGoal) Start(m *Mob) {
	g.ticksWaited = 0
	g.fox.sitting = false
	g.moveToBlock.start(m, 1.2)
}

func (g *foxEatBerriesGoal) Tick(m *Mob) {
	if g.reached {
		if g.ticksWaited >= 40 {
			g.pick(m)
		} else {
			g.ticksWaited++
		}
	} else if m.Random().NextFloat() < .05 {
		m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.fox.sniff", Volume: 1, Pitch: 1})
	}
	g.moveToBlock.tick(m, 1.2)
}

// pick ports FoxEatBerriesGoal.onReachedTarget: the fox takes a berry into
// its mouth and leaves the rest of the berries of the bush on the ground.
func (g *foxEatBerriesGoal) pick(m *Mob) {
	if mobGriefingDisabled(m.tx) {
		return
	}
	switch b := m.tx.Block(g.pos).(type) {
	case block.SweetBerries:
		count := 1 + int(m.Random().NextInt(2))
		if b.Growth == 3 {
			count++
		}
		if g.fox.held.Empty() {
			g.fox.held = item.NewStack(block.SweetBerries{}, 1)
			count--
			m.updateState()
		}
		if count > 0 {
			m.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: g.pos.Vec3Centre()}, item.NewStack(block.SweetBerries{}, count)))
		}
		m.tx.PlaySound(g.pos.Vec3Centre(), sound.Custom{Name: "block.sweet_berry_bush.pick_berries", Volume: 1, Pitch: 1})
		b.Growth = 1
		m.tx.SetBlock(g.pos, b, nil)
		m.tx.EmitGameEvent(world.GameEventBlockChange, g.pos.Vec3Centre(), world.GameEventContext{Source: m.Self})
	case block.CaveVines:
		b.PickBerries(g.pos, m.tx)
	}
}

// foxSearchForItemsGoal ports FoxSearchForItemsGoal: a fox with nothing in
// its mouth walks to an item within eight blocks.
type foxSearchForItemsGoal struct {
	goalFlags
	fox *foxState
}

// items returns the item entities within eight blocks a fox may pick up.
func (g *foxSearchForItemsGoal) items(m *Mob) []world.Entity {
	var items []world.Entity
	for e := range m.tx.EntitiesWithin(m.BBox().Grow(8)) {
		if i, ok := e.(*Ent); ok && i.H().Type() == ItemType {
			if b, ok := i.Behaviour().(*ItemBehaviour); ok && b.pickupDelay == 0 {
				items = append(items, e)
			}
		}
	}
	return items
}

func (g *foxSearchForItemsGoal) CanUse(m *Mob) bool {
	s := g.fox
	if !s.held.Empty() || m.Target() != nil {
		return false
	}
	if attacker, _ := m.LastHurtBy(); attacker != nil {
		return false
	}
	if !s.canMove() || m.Random().NextInt(int32(reducedTickDelay(10))) != 0 {
		return false
	}
	return len(g.items(m)) > 0
}

func (g *foxSearchForItemsGoal) Start(m *Mob) {
	if items := g.items(m); len(items) > 0 {
		m.AI.nav.MoveToEntity(m, items[0], 1.2)
	}
}

func (g *foxSearchForItemsGoal) Tick(m *Mob) {
	if items := g.items(m); g.fox.held.Empty() && len(items) > 0 {
		m.AI.nav.MoveToEntity(m, items[0], 1.2)
	}
}

// foxLookAtPlayerGoal ports FoxLookAtPlayerGoal, which a faceplanted or
// interested fox does not run.
type foxLookAtPlayerGoal struct {
	*LookAtPlayerGoal
	fox *foxState
}

func (g foxLookAtPlayerGoal) CanUse(m *Mob) bool {
	return g.LookAtPlayerGoal.CanUse(m) && !g.fox.faceplanted && !g.fox.interested
}

func (g foxLookAtPlayerGoal) CanContinueToUse(m *Mob) bool {
	return g.LookAtPlayerGoal.CanContinueToUse(m) && !g.fox.faceplanted && !g.fox.interested
}

// foxPerchAndSearchGoal ports PerchAndSearchGoal: an idle fox now and then
// sits down and looks around a few times.
type foxPerchAndSearchGoal struct {
	goalFlags
	fox                      *foxState
	relX, relZ               float64
	lookTime, looksRemaining int
}

func (g *foxPerchAndSearchGoal) CanUse(m *Mob) bool {
	s := g.fox
	if attacker, _ := m.LastHurtBy(); attacker != nil {
		return false
	}
	return m.Random().NextFloat() < .02 && !s.sleeping && m.Target() == nil && m.AI.nav.IsDone() && !s.alertable(m) && !s.pouncing && !s.crouching
}

func (g *foxPerchAndSearchGoal) CanContinueToUse(*Mob) bool { return g.looksRemaining > 0 }

func (g *foxPerchAndSearchGoal) Start(m *Mob) {
	g.resetLook(m)
	g.looksRemaining = 2 + int(m.Random().NextInt(3))
	g.fox.sitting = true
	m.AI.nav.Stop()
}

func (g *foxPerchAndSearchGoal) Stop(*Mob) { g.fox.sitting = false }

func (g *foxPerchAndSearchGoal) Tick(m *Mob) {
	if g.lookTime--; g.lookTime <= 0 {
		g.looksRemaining--
		g.resetLook(m)
	}
	m.AI.look.SetLookAtWith(m.data.Pos[0]+g.relX, m.EyeY(), m.data.Pos[2]+g.relZ, m.AI.MaxHeadYaw, m.AI.MaxHeadPitch)
}

func (g *foxPerchAndSearchGoal) resetLook(m *Mob) {
	r := m.Random()
	rnd := math.Pi * 2 * r.NextDouble()
	g.relX, g.relZ = math.Cos(rnd), math.Sin(rnd)
	g.lookTime = adjustedTickDelay(g, 80+int(r.NextInt(20)))
}

// foxDefendTrustedTargetGoal ports DefendTrustedTargetGoal: a fox defends a
// player it trusts from whatever hurt it.
type foxDefendTrustedTargetGoal struct {
	*NearestAttackableTargetGoal
	fox       *foxState
	attacker  *world.EntityHandle
	trusted   *world.EntityHandle
	timestamp int64
}

func (g *foxDefendTrustedTargetGoal) CanUse(m *Mob) bool {
	if g.RandomInterval > 0 && m.Random().NextInt(int32(g.RandomInterval)) != 0 {
		return false
	}
	for _, id := range g.fox.trusted {
		for p := range m.tx.Players() {
			if p.H().UUID().String() != id {
				continue
			}
			attacker, ts := ownerLastHurtBy(m.tx, p)
			g.trusted = p.H()
			if ts == g.timestamp || !g.canAttack(m, attacker, g.conditions(m)) {
				return false
			}
			g.attacker = attacker.H()
			return true
		}
	}
	return false
}

func (g *foxDefendTrustedTargetGoal) Start(m *Mob) {
	if attacker, ok := g.attacker.Entity(m.tx); ok {
		m.SetTarget(attacker)
		g.target = attacker.H()
	}
	if trusted, ok := g.trusted.Entity(m.tx); ok {
		_, g.timestamp = ownerLastHurtBy(m.tx, trusted)
	}
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.fox.aggro", Volume: 1, Pitch: 1})
	g.fox.defending, g.fox.sleeping = true, false
	g.start()
}
