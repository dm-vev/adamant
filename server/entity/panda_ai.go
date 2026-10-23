package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports the goals of Java's Panda and what Panda.tick does after
// its AI.

// Genes of pandas, as Panda.Gene ids.
const (
	pandaNormal = iota
	pandaLazy
	pandaWorried
	pandaPlayful
	pandaBrown
	pandaWeak
	pandaAggressive
)

// pandaFood reports if a stack is in #panda_food, bamboo.
func pandaFood(s item.Stack) bool { return itemNamed(s, "minecraft:bamboo") }

// pandaEatsFromGround reports if a stack is in #panda_eats_from_ground,
// bamboo and cake.
func pandaEatsFromGround(s item.Stack) bool { return pandaFood(s) || itemNamed(s, "minecraft:cake") }

// scared ports Panda.isScared: a worried panda is scared of thunderstorms.
func (s *pandaState) scared(tx *world.Tx) bool { return s.variant == pandaWorried && tx.Thundering() }

// eating ports Panda.isEating.
func (s *pandaState) eating() bool { return s.eatCounter > 0 }

// canPerformAction ports Panda.canPerformAction: a panda lying on its back,
// scared, eating, rolling or sitting does nothing else.
func (s *pandaState) canPerformAction(tx *world.Tx) bool {
	return !s.lying && !s.scared(tx) && !s.eating() && !s.rolling && !s.sitting
}

// registerGoals ports Panda.registerGoals.
func (s *pandaState) registerGoals(m *Mob) {
	ai := m.AI
	ai.moveOverride = pandaMoveControl{s}
	ai.Goals.Add(0, NewFloatGoal(m))
	s.panic = NewPanicGoal(2)
	s.panic.Causes = panicEnvironmentalCause
	ai.Goals.Add(2, pandaPanicGoal{s.panic, s})
	ai.Goals.Add(2, &pandaBreedGoal{BreedGoal: NewBreedGoal(1), panda: s})
	ai.Goals.Add(3, pandaAttackGoal{NewMeleeAttackGoal(1.2, true), s})
	ai.Goals.Add(4, NewTemptGoal(1, pandaFood, false))
	notSpectating := func(e world.Entity) bool { return isPlayerEntity(e) && !spectator(e) }
	ai.Goals.Add(6, pandaAvoidGoal{NewAvoidEntityGoal(notSpectating, 8, 2, 2), s})
	ai.Goals.Add(6, pandaAvoidGoal{NewAvoidEntityGoal(func(e world.Entity) bool { return monsterClass[e.H().Type().EncodeEntity()] }, 4, 2, 2), s})
	ai.Goals.Add(7, &pandaSitGoal{goalFlags: goalFlags{GoalMove}, panda: s})
	ai.Goals.Add(8, &pandaLieOnBackGoal{panda: s})
	ai.Goals.Add(8, &pandaSneezeGoal{panda: s})
	s.lookAtPlayer = &pandaLookAtPlayerGoal{LookAtPlayerGoal: NewLookAtPlayerGoal(6), panda: s}
	ai.Goals.Add(9, s.lookAtPlayer)
	ai.Goals.Add(10, NewRandomLookAroundGoal())
	ai.Goals.Add(12, &pandaRollGoal{goalFlags: goalFlags{GoalMove | GoalLook | GoalJump}, panda: s})
	ai.Goals.Add(13, NewFollowParentGoal(1.25))
	ai.Goals.Add(14, NewWaterAvoidingRandomStrollGoal(1))
	hurt := NewHurtByTargetGoal()
	hurt.AlertOthers = true
	// PandaHurtByTargetGoal.alertOther: only aggressive pandas join in.
	hurt.AlertOther = func(_, other *Mob, attacker world.Entity) {
		if p, ok := other.Self.(*Panda); ok && p.b().variant == pandaAggressive {
			other.SetTarget(attacker)
		}
	}
	ai.Targets.Add(1, pandaHurtByTargetGoal{hurt, s})
}

// afterTick runs what Panda.tick does after the AI: a worried panda sits
// out thunderstorms, a sulking panda looks at its target and whines, a
// panda sneezes, rolls along and eats what it holds.
func (s *pandaState) afterTick(m *Mob) {
	if s.variant == pandaWorried {
		if m.tx.Thundering() && !m.InWater() {
			s.sitting, s.eatCounter = true, 0
		} else if !s.eating() {
			s.sitting = false
		}
	}
	target := m.Target()
	if target == nil {
		s.gotBamboo, s.didBite = false, false
	}
	if s.unhappy > 0 {
		if target != nil {
			m.LookAt(target, 90, 90)
		}
		if s.unhappy == 29 || s.unhappy == 14 {
			m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.panda.cant_breed", Volume: 1, Pitch: 1})
		}
		s.unhappy--
	}
	if s.sneezing {
		if s.sneezeCounter++; s.sneezeCounter > 20 {
			s.sneezing, s.sneezeCounter = false, 0
			s.afterSneeze(m)
		} else if s.sneezeCounter == 1 {
			m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.panda.pre_sneeze", Volume: 1, Pitch: 1})
		}
	}
	if s.rolling {
		s.handleRoll(m)
	} else {
		s.rollCounter = 0
	}
	if s.sitting {
		m.SetPitch(0)
	}
	s.handleEating(m)
}

// handleRoll ports Panda.handleRoll: a rolling panda tumbles forwards in
// three hops over a second and a half.
func (s *pandaState) handleRoll(m *Mob) {
	if s.rollCounter++; s.rollCounter > 32 {
		s.rolling = false
		return
	}
	v := m.data.Vel
	switch s.rollCounter {
	case 1:
		angle := m.Yaw() * math.Pi / 180
		multiplier := float64(float32(.2))
		if s.age < 0 {
			multiplier = float64(float32(.1))
		}
		s.rollDelta = mgl64.Vec3{v[0] - math.Sin(angle)*multiplier, 0, v[2] + math.Cos(angle)*multiplier}
		m.data.Vel = s.rollDelta.Add(mgl64.Vec3{0, .27, 0})
	case 7, 15, 23:
		y := v[1]
		if m.OnGround() {
			y = .27
		}
		m.data.Vel = mgl64.Vec3{0, y, 0}
	default:
		m.data.Vel = mgl64.Vec3{s.rollDelta[0], v[1], s.rollDelta[2]}
	}
}

// afterSneeze ports Panda.afterSneeze: the sneeze startles the grown pandas
// around into jumping, and may blow out a slime ball.
func (s *pandaState) afterSneeze(m *Mob) {
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.panda.sneeze", Volume: 1, Pitch: 1})
	for e := range m.tx.EntitiesWithin(m.BBox().Grow(10)) {
		other, ok := e.(*Panda)
		if !ok {
			continue
		}
		ob := other.b()
		om := ob.ai.Mob(other.Ent, other)
		if ob.age >= 0 && om.OnGround() && !om.InWater() && ob.canPerformAction(m.tx) {
			ob.ai.jumpFromGround(om)
		}
	}
	if !m.tx.World().GameRuleBool("mob_drops") {
		return
	}
	const id = "minecraft:gameplay/panda_sneeze"
	sequence, err := loot.RandomSequence(id)
	if err != nil {
		return
	}
	drops, err := loot.Generate(id, 0, loot.Context{Entity: "minecraft:panda", Origin: m.data.Pos, Random: m.tx.LootRandomSequence(sequence, 0)})
	if err != nil {
		return
	}
	for _, stack := range drops {
		m.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: m.data.Pos}, stack))
	}
}

// handleEating ports Panda.handleEating: a sitting panda holding food now
// and then munches on it for over four seconds, finishing what it ate off
// the ground.
func (s *pandaState) handleEating(m *Mob) {
	r := m.Random()
	if !s.eating() && s.sitting && !s.scared(m.tx) && !s.held.Empty() && r.NextInt(80) == 1 {
		s.eatCounter = 1
	} else if s.held.Empty() || !s.sitting {
		s.eatCounter = 0
	}
	if !s.eating() {
		return
	}
	if s.eatCounter%5 == 0 {
		m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.panda.eat", Volume: .5 + .5*float64(r.NextInt(2)), Pitch: float64((r.NextFloat()-r.NextFloat())*.2 + 1)})
	}
	if s.eatCounter > 80 && r.NextInt(20) == 1 {
		if s.eatCounter > 100 && pandaEatsFromGround(s.held) {
			s.held = item.Stack{}
			s.viewHeld(m)
			m.tx.EmitGameEvent(world.GameEventEat, m.data.Pos, world.GameEventContext{Source: m.Self})
			s.sitting = false
		}
		s.eatCounter = 0
		return
	}
	s.eatCounter++
}

// viewHeld shows viewers what the panda holds.
func (s *pandaState) viewHeld(m *Mob) {
	viewers := m.tx.Viewers(m.data.Pos)
	for _, v := range viewers {
		v.ViewEntityItems(m.Self)
	}
	m.tx.ReleaseViewers(viewers)
}

// tryToSit ports Panda.tryToSit.
func (s *pandaState) tryToSit(m *Mob) {
	if !m.InWater() {
		m.SetZza(0)
		m.AI.nav.Stop()
		s.sitting = true
	}
}

// doHurtTarget ports Panda.doHurtTarget: a panda bites for 6 damage, and
// one that is not aggressive only bites back once.
func (s *pandaState) doHurtTarget(m *Mob, target world.Entity) bool {
	if s.variant != pandaAggressive {
		s.didBite = true
	}
	l, ok := target.(Living)
	if !ok {
		return false
	}
	if _, hurt := l.Hurt(6, AttackDamageSource{Attacker: m.Self}); !hurt {
		return false
	}
	l.KnockBack(m.data.Pos, .4, .4)
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.panda.bite", Volume: 1, Pitch: 1})
	return true
}

// pandaMoveControl ports Panda.PandaMoveControl: a panda busy with something
// does not walk.
type pandaMoveControl struct{ panda *pandaState }

func (c pandaMoveControl) tick(m *Mob) {
	if c.panda.canPerformAction(m.tx) {
		m.AI.move.tick(m)
	}
}

// pandaPanicGoal ports Panda.PandaPanicGoal, which a panda sitting down
// stops.
type pandaPanicGoal struct {
	*PanicGoal
	panda *pandaState
}

func (g pandaPanicGoal) CanContinueToUse(m *Mob) bool {
	if g.panda.sitting {
		m.AI.nav.Stop()
		return false
	}
	return g.PanicGoal.CanContinueToUse(m)
}

// pandaBreedGoal ports Panda.PandaBreedGoal: a panda only breeds with bamboo
// growing within eight blocks, and otherwise sulks at the nearest player at
// most every 30 seconds.
type pandaBreedGoal struct {
	*BreedGoal
	panda           *pandaState
	unhappyCooldown int
}

func (g *pandaBreedGoal) CanUse(m *Mob) bool {
	s := g.panda
	if !g.BreedGoal.CanUse(m) || s.unhappy != 0 {
		return false
	}
	if pandaBambooNearby(m.tx, m.BlockPos()) {
		return true
	}
	if g.unhappyCooldown <= m.AI.tickCount {
		s.unhappy, g.unhappyCooldown = 32, m.AI.tickCount+600
		c := forNonCombat()
		c.rangeLimit = 8
		if p := nearestPlayer(m, c); p != nil {
			s.lookAtPlayer.lookAt = p.H()
		} else {
			s.lookAtPlayer.lookAt = nil
		}
	}
	return false
}

// pandaBambooNearby ports PandaBreedGoal.canFindBamboo: bamboo in a square of
// radius seven around the panda, up to two blocks above it.
func pandaBambooNearby(tx *world.Tx, pos cube.Pos) bool {
	for y := range 3 {
		for x := -7; x <= 7; x++ {
			for z := -7; z <= 7; z++ {
				if _, ok := tx.Block(pos.Add(cube.Pos{x, y, z})).(block.Bamboo); ok {
					return true
				}
			}
		}
	}
	return false
}

// pandaAttackGoal ports Panda.PandaAttackGoal, which a busy panda does not
// start.
type pandaAttackGoal struct {
	*MeleeAttackGoal
	panda *pandaState
}

func (g pandaAttackGoal) CanUse(m *Mob) bool {
	return g.panda.canPerformAction(m.tx) && g.MeleeAttackGoal.CanUse(m)
}

// pandaAvoidGoal ports Panda.PandaAvoidGoal, which only a worried panda free
// to act runs.
type pandaAvoidGoal struct {
	*AvoidEntityGoal
	panda *pandaState
}

func (g pandaAvoidGoal) CanUse(m *Mob) bool {
	return g.panda.variant == pandaWorried && g.panda.canPerformAction(m.tx) && g.AvoidEntityGoal.CanUse(m)
}

// pandaHurtByTargetGoal ports Panda.PandaHurtByTargetGoal: a panda forgets
// its attacker once it bit it or was given bamboo.
type pandaHurtByTargetGoal struct {
	*HurtByTargetGoal
	panda *pandaState
}

func (g pandaHurtByTargetGoal) CanContinueToUse(m *Mob) bool {
	if !g.panda.gotBamboo && !g.panda.didBite {
		return g.HurtByTargetGoal.CanContinueToUse(m)
	}
	m.SetTarget(nil)
	return false
}

// pandaLieOnBackGoal ports Panda.PandaLieOnBackGoal: a lazy panda now and
// then lies on its back for a while.
type pandaLieOnBackGoal struct {
	goalFlags
	panda    *pandaState
	cooldown int
}

func (g *pandaLieOnBackGoal) CanUse(m *Mob) bool {
	s := g.panda
	return g.cooldown < m.AI.tickCount && s.variant == pandaLazy && s.canPerformAction(m.tx) && m.Random().NextInt(int32(reducedTickDelay(400))) == 1
}

func (g *pandaLieOnBackGoal) CanContinueToUse(m *Mob) bool {
	r := m.Random()
	if m.InWater() || g.panda.variant != pandaLazy && r.NextInt(int32(reducedTickDelay(600))) == 1 {
		return false
	}
	return r.NextInt(int32(reducedTickDelay(2000))) != 1
}

func (g *pandaLieOnBackGoal) Start(*Mob) { g.panda.lying, g.cooldown = true, 0 }

func (g *pandaLieOnBackGoal) Stop(m *Mob) { g.panda.lying, g.cooldown = false, m.AI.tickCount+200 }

// pandaLookAtPlayerGoal ports Panda.PandaLookAtPlayerGoal, which keeps
// looking at the player a sulking panda picked and only a panda free to act
// starts.
type pandaLookAtPlayerGoal struct {
	*LookAtPlayerGoal
	panda *pandaState
}

func (g *pandaLookAtPlayerGoal) CanUse(m *Mob) bool {
	if m.Random().NextFloat() >= float32(g.Probability) {
		return false
	}
	if g.lookAt == nil {
		c := forNonCombat()
		c.rangeLimit = g.LookDistance
		if p := nearestPlayer(m, c); p != nil {
			g.lookAt = p.H()
		}
	}
	return g.panda.canPerformAction(m.tx) && g.lookAt != nil
}

func (g *pandaLookAtPlayerGoal) CanContinueToUse(m *Mob) bool {
	return g.lookAt != nil && g.LookAtPlayerGoal.CanContinueToUse(m)
}

func (g *pandaLookAtPlayerGoal) Tick(m *Mob) {
	if g.lookAt != nil {
		g.LookAtPlayerGoal.Tick(m)
	}
}

// pandaRollGoal ports Panda.PandaRollGoal: a cub or playful panda rolls over
// a ledge ahead of it, and now and then anyway.
type pandaRollGoal struct {
	goalFlags
	panda *pandaState
}

func (g *pandaRollGoal) CanUse(m *Mob) bool {
	s := g.panda
	if s.age >= 0 && s.variant != pandaPlayful || !m.OnGround() || !s.canPerformAction(m.tx) {
		return false
	}
	angle := m.Yaw() * math.Pi / 180
	xDir, zDir := -math.Sin(angle), math.Cos(angle)
	step := func(d float64) int {
		if math.Abs(d) > .5 {
			if d > 0 {
				return 1
			}
			return -1
		}
		return 0
	}
	if isAir(m.tx.Block(m.BlockPos().Add(cube.Pos{step(xDir), -1, step(zDir)}))) {
		return true
	}
	r := m.Random()
	if s.variant == pandaPlayful && r.NextInt(int32(reducedTickDelay(60))) == 1 {
		return true
	}
	return r.NextInt(int32(reducedTickDelay(500))) == 1
}

func (g *pandaRollGoal) CanContinueToUse(*Mob) bool { return false }

func (g *pandaRollGoal) Start(*Mob) { g.panda.rolling = true }

func (g *pandaRollGoal) IsInterruptable() bool { return false }

// pandaSitGoal ports Panda.PandaSitGoal: a panda holding food, or one with
// food on the ground within six blocks, sits down to eat it and tosses away
// what it did not finish.
type pandaSitGoal struct {
	goalFlags
	panda    *pandaState
	cooldown int
}

// pandaGroundFood returns the item entities of food within distance a panda
// eats off the ground, as Panda.canPickUpAndEat selects.
func pandaGroundFood(m *Mob, distance float64) []world.Entity {
	var items []world.Entity
	for e := range m.tx.EntitiesWithin(m.BBox().Grow(distance)) {
		i, ok := e.(*Ent)
		if !ok || i.H().Type() != ItemType {
			continue
		}
		if b, ok := i.Behaviour().(*ItemBehaviour); ok && b.pickupDelay == 0 && pandaEatsFromGround(b.i) {
			items = append(items, e)
		}
	}
	return items
}

func (g *pandaSitGoal) CanUse(m *Mob) bool {
	s := g.panda
	if g.cooldown > m.AI.tickCount || s.age < 0 || m.InWater() || !s.canPerformAction(m.tx) || s.unhappy > 0 {
		return false
	}
	return !s.held.Empty() || len(pandaGroundFood(m, 6)) > 0
}

func (g *pandaSitGoal) CanContinueToUse(m *Mob) bool {
	r := m.Random()
	if m.InWater() || g.panda.variant != pandaLazy && r.NextInt(int32(reducedTickDelay(600))) == 1 {
		return false
	}
	return r.NextInt(int32(reducedTickDelay(2000))) != 1
}

func (g *pandaSitGoal) Tick(m *Mob) {
	if !g.panda.sitting && !g.panda.held.Empty() {
		g.panda.tryToSit(m)
	}
}

func (g *pandaSitGoal) Start(m *Mob) {
	if g.panda.held.Empty() {
		if items := pandaGroundFood(m, 8); len(items) > 0 {
			m.AI.nav.MoveToEntity(m, items[0], float64(float32(1.2)))
		}
	} else {
		g.panda.tryToSit(m)
	}
	g.cooldown = 0
}

func (g *pandaSitGoal) Stop(m *Mob) {
	s := g.panda
	if !s.held.Empty() {
		m.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: m.data.Pos}, s.held))
		s.held = item.Stack{}
		s.viewHeld(m)
		r := m.Random()
		wait := int(r.NextInt(150)) + 10
		if s.variant == pandaLazy {
			wait = int(r.NextInt(50)) + 10
		}
		g.cooldown = m.AI.tickCount + wait*20
	}
	s.sitting = false
}

// pandaSneezeGoal ports Panda.PandaSneezeGoal: now and then a cub sneezes,
// a weak one far more often.
type pandaSneezeGoal struct {
	goalFlags
	panda *pandaState
}

func (g *pandaSneezeGoal) CanUse(m *Mob) bool {
	s := g.panda
	if s.age >= 0 || !s.canPerformAction(m.tx) {
		return false
	}
	r := m.Random()
	if s.variant == pandaWeak && r.NextInt(int32(reducedTickDelay(500))) == 1 {
		return true
	}
	return r.NextInt(int32(reducedTickDelay(6000))) == 1
}

func (g *pandaSneezeGoal) CanContinueToUse(*Mob) bool { return false }

func (g *pandaSneezeGoal) Start(*Mob) { g.panda.sneezing = true }
