package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports the goals of Java's Evoker and SpellcasterIllager, Vex
// and Ravager.

// illagerSpell is SpellcasterIllager.IllagerSpell.
type illagerSpell uint8

const (
	spellNone illagerSpell = iota
	spellSummonVex
	spellFangs
	spellWololo
)

// spellColours are the colours of the particles of each spell.
var spellColours = [...][3]float64{{0, 0, 0}, {.7, .7, .8}, {.4, .3, .35}, {.7, .5, .2}}

// Casting reports if the evoker casts a spell.
func (m *HostileMob) Casting() bool { return m.state().spell != spellNone }

// SpellColour returns the colour of the spell the evoker casts.
func (m *HostileMob) SpellColour() [3]float64 { return spellColours[m.state().spell] }

func (b *hostileState) setSpell(m *Mob, spell illagerSpell) {
	if b.spell != spell {
		b.spell = spell
		m.updateState()
	}
}

// registerEvokerGoals ports Evoker.registerGoals.
func registerEvokerGoals(m *Mob, b *hostileState) {
	ai := m.AI
	registerRaiderGoals(m)
	ai.Goals.Add(0, NewFloatGoal(m))
	ai.Goals.Add(1, &evokerCastingSpellGoal{goalFlags{GoalMove | GoalLook}})
	ai.Goals.Add(2, NewAvoidEntityGoal(isPlayerEntity, 8, .6, 1))
	ai.Goals.Add(3, NewAvoidEntityGoal(entityIs("minecraft:creaking"), 8, .6, 1))
	ai.Goals.Add(4, &evokerSummonSpellGoal{spellUseGoal{castWarmup: 20, castingTime: 100, castingInterval: 340, prepareSound: "entity.evoker.prepare_summon", spell: spellSummonVex}})
	ai.Goals.Add(5, &spellUseGoal{castWarmup: 20, castingTime: 40, castingInterval: 100, prepareSound: "entity.evoker.prepare_attack", spell: spellFangs, perform: evokerFangsSpell})
	ai.Goals.Add(6, &evokerWololoSpellGoal{spellUseGoal{castWarmup: 40, castingTime: 60, castingInterval: 140, prepareSound: "entity.evoker.prepare_wololo", spell: spellWololo}})
	ai.Goals.Add(8, NewRandomStrollGoal(.6))
	lookPlayer := NewLookAtPlayerGoal(3)
	lookPlayer.Probability = 1
	ai.Goals.Add(9, lookPlayer)
	lookMob := NewLookAtPlayerGoal(8)
	lookMob.IsType = isMobEntity
	ai.Goals.Add(10, lookMob)
	ai.Targets.Add(1, raiderHurtByTarget())
	players := NewNearestAttackableTargetGoal(nil, true)
	players.unseenMemoryTicks = 300
	ai.Targets.Add(2, players)
	villagers := NewNearestAttackableTargetGoal(isVillagerEntity, false)
	villagers.unseenMemoryTicks = 300
	ai.Targets.Add(3, villagers)
	ai.Targets.Add(3, NewNearestAttackableTargetGoal(entityIs("minecraft:iron_golem"), false))
}

// evokerCastingSpellGoal ports Evoker.EvokerCastingSpellGoal: an evoker
// stands still facing its target while it casts.
type evokerCastingSpellGoal struct{ goalFlags }

func (g *evokerCastingSpellGoal) CanUse(m *Mob) bool { return hostileOf(m).spellTicks > 0 }

func (g *evokerCastingSpellGoal) Start(m *Mob) { m.AI.nav.Stop() }

func (g *evokerCastingSpellGoal) Stop(m *Mob) { hostileOf(m).setSpell(m, spellNone) }

func (g *evokerCastingSpellGoal) Tick(m *Mob) {
	b := hostileOf(m)
	if target := m.Target(); target != nil {
		m.AI.look.SetLookAtEntity(m, target)
	} else if b.wololoTarget != nil {
		if sheep, ok := b.wololoTarget.Entity(m.tx); ok {
			m.AI.look.SetLookAtEntity(m, sheep)
		}
	}
}

// spellUseGoal ports SpellcasterIllager.SpellcasterUseSpellGoal: after a
// warmup the spell is cast, and the evoker may cast it again only after an
// interval.
type spellUseGoal struct {
	goalFlags
	castWarmup, castingTime, castingInterval int
	prepareSound                             string
	spell                                    illagerSpell
	perform                                  func(m *Mob)

	warmupDelay    int
	nextAttackTick int
}

func (g *spellUseGoal) CanUse(m *Mob) bool {
	target := m.Target()
	return target != nil && entityAlive(target) && hostileOf(m).spellTicks <= 0 && m.AI.tickCount >= g.nextAttackTick
}

func (g *spellUseGoal) CanContinueToUse(m *Mob) bool {
	target := m.Target()
	return target != nil && entityAlive(target) && g.warmupDelay > 0
}

func (g *spellUseGoal) Start(m *Mob) {
	b := hostileOf(m)
	g.warmupDelay = reducedTickDelay(g.castWarmup)
	b.spellTicks = g.castingTime
	g.nextAttackTick = m.AI.tickCount + g.castingInterval
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: g.prepareSound, Volume: 1, Pitch: 1})
	b.setSpell(m, g.spell)
}

func (g *spellUseGoal) Tick(m *Mob) {
	if g.warmupDelay--; g.warmupDelay == 0 {
		g.perform(m)
		m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.evoker.cast_spell", Volume: 1, Pitch: 1})
	}
}

// evokerFangsSpell ports Evoker.EvokerAttackSpellGoal.performSpellCasting:
// a ring of fangs around an evoker whose target is close, or a line of
// them towards it.
func evokerFangsSpell(m *Mob) {
	target := m.Target()
	if target == nil {
		return
	}
	tp := target.Position()
	minY := math.Min(tp[1], m.data.Pos[1])
	maxY := math.Max(tp[1], m.data.Pos[1]) + 1
	angle := math.Atan2(tp[2]-m.data.Pos[2], tp[0]-m.data.Pos[0])
	if m.DistanceToSqr(target) < 9 {
		for i := range 5 {
			a := angle + float64(i)*math.Pi*.4
			evokerFang(m, m.data.Pos[0]+math.Cos(a)*1.5, m.data.Pos[2]+math.Sin(a)*1.5, minY, maxY, a, 0)
		}
		for i := range 8 {
			a := angle + float64(i)*math.Pi*2/8 + math.Pi*2/5
			evokerFang(m, m.data.Pos[0]+math.Cos(a)*2.5, m.data.Pos[2]+math.Sin(a)*2.5, minY, maxY, a, 3)
		}
		return
	}
	for i := range 16 {
		reach := 1.25 * float64(i+1)
		evokerFang(m, m.data.Pos[0]+math.Cos(angle)*reach, m.data.Pos[2]+math.Sin(angle)*reach, minY, maxY, angle, i)
	}
}

// evokerFang ports EvokerAttackSpellGoal.createSpellEntity: a fang on the
// first sturdy floor at most a block below the lower of the evoker and its
// target.
func evokerFang(m *Mob, x, z, minY, maxY, angle float64, delay int) {
	pos := cube.PosFromVec3(mgl64.Vec3{x, maxY, z})
	for {
		below := pos.Side(cube.FaceDown)
		if m.tx.Block(below).Model().FaceSolid(below, cube.FaceUp, m.tx) {
			top := 0.0
			for _, box := range m.tx.Block(pos).Model().BBox(pos, m.tx) {
				top = math.Max(top, box.Max()[1])
			}
			at := mgl64.Vec3{x, float64(pos[1]) + top, z}
			opts := world.EntitySpawnOpts{Position: at, Rotation: cube.Rotation{angle * 180 / math.Pi, 0}}
			m.tx.AddEntity(NewEvokerFangs(opts, m.Self, delay))
			m.tx.EmitGameEvent(world.GameEventEntityPlace, at, world.GameEventContext{Source: m.Self})
			return
		}
		pos = below
		if pos[1] < int(math.Floor(minY))-1 {
			return
		}
	}
}

// evokerSummonSpellGoal ports Evoker.EvokerSummonSpellGoal: an evoker with
// few vexes around summons three more.
type evokerSummonSpellGoal struct{ spellUseGoal }

func (g *evokerSummonSpellGoal) CanUse(m *Mob) bool {
	if !g.spellUseGoal.CanUse(m) {
		return false
	}
	vexes := 0
	for e := range m.tx.EntitiesWithin(m.BBox().Grow(16)) {
		if e.H().Type() == VexType && entityAlive(e) && m.DistanceToSqr(e) <= 256 {
			vexes++
		}
	}
	return int(m.Random().NextInt(8))+1 > vexes
}

func (g *evokerSummonSpellGoal) Tick(m *Mob) {
	if g.warmupDelay--; g.warmupDelay == 0 {
		evokerSummonVexes(m)
		m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.evoker.cast_spell", Volume: 1, Pitch: 1})
	}
}

// evokerSummonVexes ports EvokerSummonSpellGoal.performSpellCasting: three
// vexes bound to the evoker that live 30 to 120 seconds.
func evokerSummonVexes(m *Mob) {
	r := m.Random()
	for range 3 {
		pos := m.BlockPos().Add(cube.Pos{-2 + int(r.NextInt(5)), 1, -2 + int(r.NextInt(5))})
		h := NewVex(world.EntitySpawnOpts{Position: pos.Vec3Middle(), SpawnReason: "mob_summoned"})
		vex, ok := m.tx.AddEntity(h).(*Vex)
		if !ok {
			continue
		}
		s := vex.state()
		s.vexOwner = m.H()
		s.vexBound, s.vexHasBound = pos, true
		s.vexLimitedLife, s.vexLifeTicks = true, 20*(30+int(r.NextInt(90)))
		m.tx.EmitGameEvent(world.GameEventEntityPlace, pos.Vec3Middle(), world.GameEventContext{Source: m.Self})
	}
}

// evokerWololoSpellGoal ports Evoker.EvokerWololoSpellGoal: an idle evoker
// turns a blue sheep red.
type evokerWololoSpellGoal struct{ spellUseGoal }

func (g *evokerWololoSpellGoal) CanUse(m *Mob) bool {
	b := hostileOf(m)
	if m.Target() != nil || b.spellTicks > 0 || m.AI.tickCount < g.nextAttackTick || mobGriefingDisabled(m.tx) {
		return false
	}
	c := forNonCombat()
	c.rangeLimit = 16
	var sheep []world.Entity
	for e := range m.tx.EntitiesWithin(m.BBox().GrowVec3(mgl64.Vec3{16, 4, 16})) {
		if s, ok := e.(*Sheep); ok && s.behaviour().color == sheepColor("blue") && c.test(m, e) {
			sheep = append(sheep, e)
		}
	}
	if len(sheep) == 0 {
		return false
	}
	b.wololoTarget = sheep[m.Random().NextInt(int32(len(sheep)))].H()
	return true
}

func (g *evokerWololoSpellGoal) CanContinueToUse(m *Mob) bool {
	return hostileOf(m).wololoTarget != nil && g.warmupDelay > 0
}

func (g *evokerWololoSpellGoal) Stop(m *Mob) { hostileOf(m).wololoTarget = nil }

func (g *evokerWololoSpellGoal) Tick(m *Mob) {
	if g.warmupDelay--; g.warmupDelay != 0 {
		return
	}
	if target := hostileOf(m).wololoTarget; target != nil {
		if s, ok := target.Entity(m.tx); ok && entityAlive(s) {
			sheep := s.(*Sheep)
			sheep.behaviour().color = sheepColor("red")
			sheep.updateState()
		}
	}
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.evoker.cast_spell", Volume: 1, Pitch: 1})
}

// registerVexGoals ports Vex.registerGoals.
func registerVexGoals(m *Mob, b *hostileState) {
	ai := m.AI
	ai.moveOverride = &b.vexMove
	ai.Goals.Add(0, NewFloatGoal(m))
	ai.Goals.Add(4, &vexChargeAttackGoal{goalFlags{GoalMove}})
	ai.Goals.Add(8, &vexRandomMoveGoal{goalFlags{GoalMove}})
	lookPlayer := NewLookAtPlayerGoal(3)
	lookPlayer.Probability = 1
	ai.Goals.Add(9, lookPlayer)
	lookMob := NewLookAtPlayerGoal(8)
	lookMob.IsType = isMobEntity
	ai.Goals.Add(10, lookMob)
	ai.Targets.Add(1, raiderHurtByTarget())
	ai.Targets.Add(2, &vexCopyOwnerTargetGoal{targetGoal: newTargetGoal(false, false)})
	ai.Targets.Add(3, NewNearestAttackableTargetGoal(nil, true))
}

// tickVexLife ports the limited life of Vex.tick: once its life runs out, a
// vex starves a point a second.
func (b *hostileState) tickVexLife(m *Mob) {
	if !b.vexLimitedLife {
		return
	}
	if b.vexLifeTicks--; b.vexLifeTicks <= 0 {
		b.vexLifeTicks = 20
		if l, ok := m.Self.(Living); ok {
			l.Hurt(1, StarvationDamageSource{})
		}
	}
}

// Charging reports if the vex charges at its target or the pillager loads
// its crossbow.
func (m *HostileMob) Charging() bool {
	s := m.state()
	return s.vexCharging || s.chargingCrossbow
}

// CrossbowCharged reports if the pillager holds its crossbow loaded.
func (m *HostileMob) CrossbowCharged() bool { return m.state().crossbowCharged }

// Celebrating reports if the raider celebrates a won raid.
func (m *HostileMob) Celebrating() bool { return m.state().celebrating }

func (b *hostileState) setVexCharging(m *Mob, charging bool) {
	if b.vexCharging != charging {
		b.vexCharging = charging
		m.updateState()
	}
}

// vexMoveControl ports Vex.VexMoveControl: a vex flies straight at where it
// wants to be, through blocks.
type vexMoveControl struct {
	wanted mgl64.Vec3
	speed  float64
	moveTo bool
}

func (c *vexMoveControl) setWantedPosition(pos mgl64.Vec3, speed float64) {
	c.wanted, c.speed, c.moveTo = pos, speed, true
}

func (c *vexMoveControl) tick(m *Mob) {
	if !c.moveTo {
		return
	}
	delta := c.wanted.Sub(m.data.Pos)
	length := delta.Len()
	box := m.BBox()
	if length < (box.Width()+box.Height()+box.Length())/3 {
		c.moveTo = false
		m.data.Vel = m.data.Vel.Mul(.5)
		return
	}
	m.data.Vel = m.data.Vel.Add(delta.Mul(c.speed * .05 / length))
	d := m.data.Vel
	if target := m.Target(); target != nil {
		d = target.Position().Sub(m.data.Pos)
	}
	yaw := -math.Atan2(d[0], d[2]) * 180 / math.Pi
	m.SetYaw(yaw)
	m.AI.bodyYaw = yaw
}

// vexChargeAttackGoal ports Vex.VexChargeAttackGoal: a vex charges at the
// eyes of its target and strikes when it reaches it.
type vexChargeAttackGoal struct{ goalFlags }

func (g *vexChargeAttackGoal) CanUse(m *Mob) bool {
	target := m.Target()
	if target == nil || !entityAlive(target) || hostileOf(m).vexMove.moveTo || m.Random().NextInt(int32(reducedTickDelay(7))) != 0 {
		return false
	}
	return m.DistanceToSqr(target) > 4
}

func (g *vexChargeAttackGoal) CanContinueToUse(m *Mob) bool {
	b := hostileOf(m)
	target := m.Target()
	return b.vexMove.moveTo && b.vexCharging && target != nil && entityAlive(target)
}

func (g *vexChargeAttackGoal) Start(m *Mob) {
	b := hostileOf(m)
	if target := m.Target(); target != nil {
		b.vexMove.setWantedPosition(mgl64.Vec3{target.Position()[0], entityEyeY(target), target.Position()[2]}, 1)
	}
	b.setVexCharging(m, true)
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.vex.charge", Volume: 1, Pitch: 1})
}

func (g *vexChargeAttackGoal) Stop(m *Mob) { hostileOf(m).setVexCharging(m, false) }

func (g *vexChargeAttackGoal) RequiresUpdateEveryTick() bool { return true }

func (g *vexChargeAttackGoal) Tick(m *Mob) {
	target := m.Target()
	if target == nil {
		return
	}
	b := hostileOf(m)
	if m.BBox().IntersectsWith(target.H().Type().BBox(target).Translate(target.Position())) {
		m.doHurtTarget(target)
		b.setVexCharging(m, false)
	} else if m.DistanceToSqr(target) < 9 {
		b.vexMove.setWantedPosition(mgl64.Vec3{target.Position()[0], entityEyeY(target), target.Position()[2]}, 1)
	}
}

// vexCopyOwnerTargetGoal ports Vex.VexCopyOwnerTargetGoal: a vex attacks
// what its evoker attacks.
type vexCopyOwnerTargetGoal struct{ targetGoal }

func (g *vexCopyOwnerTargetGoal) ownerTarget(m *Mob) world.Entity {
	b := hostileOf(m)
	if b.vexOwner == nil {
		return nil
	}
	owner, ok := b.vexOwner.Entity(m.tx)
	if !ok {
		return nil
	}
	if om, ok := mobOf(owner); ok {
		return om.Target()
	}
	return nil
}

func (g *vexCopyOwnerTargetGoal) CanUse(m *Mob) bool {
	c := forNonCombat()
	c.ignoreSight, c.ignoreInvis = true, true
	return g.canAttack(m, g.ownerTarget(m), c)
}

func (g *vexCopyOwnerTargetGoal) CanContinueToUse(m *Mob) bool { return g.canContinue(m) }

func (g *vexCopyOwnerTargetGoal) Start(m *Mob) {
	m.SetTarget(g.ownerTarget(m))
	g.start()
}

func (g *vexCopyOwnerTargetGoal) Stop(m *Mob) { g.stop(m) }

// vexRandomMoveGoal ports Vex.VexRandomMoveGoal: a vex flits to a random
// empty block near where it was summoned.
type vexRandomMoveGoal struct{ goalFlags }

func (g *vexRandomMoveGoal) CanUse(m *Mob) bool {
	return !hostileOf(m).vexMove.moveTo && m.Random().NextInt(int32(reducedTickDelay(7))) == 0
}

func (g *vexRandomMoveGoal) CanContinueToUse(*Mob) bool { return false }

func (g *vexRandomMoveGoal) Tick(m *Mob) {
	b := hostileOf(m)
	origin := m.BlockPos()
	if b.vexHasBound {
		origin = b.vexBound
	}
	r := m.Random()
	for range 3 {
		pos := origin.Add(cube.Pos{int(r.NextInt(15)) - 7, int(r.NextInt(11)) - 5, int(r.NextInt(15)) - 7})
		if _, air := m.tx.Block(pos).(block.Air); !air {
			continue
		}
		at := pos.Vec3Centre()
		b.vexMove.setWantedPosition(at, .25)
		if m.Target() == nil {
			m.AI.look.SetLookAtWith(at[0], at[1], at[2], 180, 20)
		}
		return
	}
}

// registerRavagerGoals ports Ravager.registerGoals.
func registerRavagerGoals(m *Mob, b *hostileState) {
	ai := m.AI
	registerRaiderGoals(m)
	ai.SetPathfindingMalus(PathLeaves, 0)
	ai.Goals.Add(0, NewFloatGoal(m))
	ai.Goals.Add(4, NewMeleeAttackGoal(1, true))
	ai.Goals.Add(5, NewWaterAvoidingRandomStrollGoal(.4))
	ai.Goals.Add(6, NewLookAtPlayerGoal(6))
	lookMob := NewLookAtPlayerGoal(8)
	lookMob.IsType = isMobEntity
	ai.Goals.Add(10, lookMob)
	ai.Targets.Add(2, raiderHurtByTarget())
	ai.Targets.Add(3, NewNearestAttackableTargetGoal(nil, true))
	villagers := NewNearestAttackableTargetGoal(isVillagerEntity, true)
	villagers.Selector = func(_ *Mob, target world.Entity) bool { return !mobIsBaby(target) }
	ai.Targets.Add(4, villagers)
	ai.Targets.Add(4, NewNearestAttackableTargetGoal(entityIs("minecraft:iron_golem"), true))
}

// immobile ports Ravager.isImmobile: a ravager stands still while it bites,
// is stunned or roars.
func (b *hostileState) immobile() bool {
	return b.kind == "minecraft:ravager" && (b.ravagerAttack > 0 || b.ravagerStun > 0 || b.ravagerRoar > 0)
}

// blind ports Ravager.hasLineOfSight: a stunned or roaring ravager sees
// nothing.
func (b *hostileState) blind() bool {
	return b.kind == "minecraft:ravager" && (b.ravagerStun > 0 || b.ravagerRoar > 0)
}

// Stunned reports if the ravager is stunned.
func (m *HostileMob) Stunned() bool { return m.state().ravagerStun > 0 }

// Roaring reports if the ravager roars.
func (m *HostileMob) Roaring() bool { return m.state().ravagerRoar > 0 }

// tickRavager ports what Ravager.aiStep does after the AI: a ravager speeds
// up when hunting, tramples leaves it runs into, and counts down its bite,
// stun and roar.
func (b *hostileState) tickRavager(m *Mob) {
	if !m.Alive() {
		return
	}
	if b.immobile() {
		b.speed = 0
	} else {
		maxSpeed := .3
		if m.Target() != nil {
			maxSpeed = .35
		}
		b.speed += (maxSpeed - b.speed) * .1
	}
	if m.AI.collidedHorizontally && !mobGriefingDisabled(m.tx) {
		destroyed := false
		box := m.BBox().Grow(.2)
		for x := int(math.Floor(box.Min()[0])); x <= int(math.Floor(box.Max()[0])); x++ {
			for y := int(math.Floor(box.Min()[1])); y <= int(math.Floor(box.Max()[1])); y++ {
				for z := int(math.Floor(box.Min()[2])); z <= int(math.Floor(box.Max()[2])); z++ {
					pos := cube.Pos{x, y, z}
					if _, leaves := m.tx.Block(pos).(block.Leaves); leaves {
						breakBlockDropping(m.tx, pos)
						destroyed = true
					}
				}
			}
		}
		if !destroyed && m.OnGround() {
			m.AI.jumpFromGround(m)
		}
	}
	changed := false
	if b.ravagerRoar > 0 {
		if b.ravagerRoar--; b.ravagerRoar == 10 {
			b.ravagerRoarAttack(m)
		}
		changed = b.ravagerRoar == 0
	}
	if b.ravagerAttack > 0 {
		b.ravagerAttack--
	}
	if b.ravagerStun > 0 {
		if b.ravagerStun--; b.ravagerStun == 0 {
			m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.ravager.roar", Volume: 1, Pitch: 1})
			b.ravagerRoar = 20
			changed = true
		}
	}
	if changed {
		m.updateState()
	}
}

// ravagerRoarAttack ports Ravager.roar: everything around a ravager is
// knocked away, and all but illagers take damage.
func (b *hostileState) ravagerRoarAttack(m *Mob) {
	for e := range m.tx.EntitiesWithin(m.BBox().Grow(4)) {
		l, ok := e.(Living)
		if !ok || e.H().Type() == RavagerType || !entityAlive(e) || (mobGriefingDisabled(m.tx) && e.H().Type().EncodeEntity() == "minecraft:armor_stand") {
			continue
		}
		if !hostileIllager(e.H().Type().EncodeEntity()) {
			l.Hurt(6, AttackDamageSource{Attacker: m.Self})
		}
		if !isPlayerEntity(e) {
			ravagerStrongKnockback(m, e)
		}
	}
	m.tx.EmitGameEvent(world.GameEventEntityAction, m.data.Pos, world.GameEventContext{Source: m.Self})
}

// ravagerStrongKnockback ports Ravager.strongKnockback.
func ravagerStrongKnockback(m *Mob, e world.Entity) {
	d := e.Position().Sub(m.data.Pos)
	dd := math.Max(d[0]*d[0]+d[2]*d[2], .001)
	if v, ok := e.(interface {
		Velocity() mgl64.Vec3
		SetVelocity(mgl64.Vec3)
	}); ok {
		v.SetVelocity(v.Velocity().Add(mgl64.Vec3{d[0] / dd * 4, .2, d[2] / dd * 4}))
	}
}

// breakBlockDropping ports Level.destroyBlock with drops: the block breaks
// with its particles and drops what it drops by hand.
func breakBlockDropping(tx *world.Tx, pos cube.Pos) {
	b := tx.Block(pos)
	tx.SetBlock(pos, nil, nil)
	tx.AddParticle(pos.Vec3Centre(), particle.BlockBreak{Block: b})
	if breakable, ok := b.(block.Breakable); ok && breakable.BreakInfo().Drops != nil {
		for _, stack := range breakable.BreakInfo().Drops(item.ToolNone{}, nil) {
			tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: pos.Vec3Centre()}, stack))
		}
	}
}
