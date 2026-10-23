package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports the goals Java's Animal subclasses share and the MobAI
// they run on.

// animalAI is the MobAI of an animal and the goals other code asks about.
type animalAI struct {
	ai    MobAI
	panic *PanicGoal
}

func (a *animalAI) mobAI() *MobAI { return &a.ai }

// panicking reports if the animal runs from danger, as PanicGoal.isRunning.
func (a *animalAI) panicking() bool { return a.panic != nil && a.panic.running }

// walkTargetValue ports Animal.getWalkTargetValue: animals stroll to grass
// and otherwise to light.
func (a *animalAI) walkTargetValue(m *Mob, pos cube.Pos) float64 {
	return animalWalkTargetValue(m, pos)
}

// animalTick runs what AgeableMob.aiStep and Animal.aiStep do, the ageing
// and love of the animal, and its MobAI.
func animalTick(e *Ent, tx *world.Tx, self breeder, a *animalAI, register func(m *Mob)) *Movement {
	tickAgeing(self, tx)
	return a.ai.Tick(e, self, register)
}

// animalSpec are the speeds of the goals an animal registers as
// AbstractCow does: PanicGoal, BreedGoal, TemptGoal, FollowParentGoal and
// WaterAvoidingRandomStrollGoal, with LookAtPlayerGoal and
// RandomLookAroundGoal after them.
type animalSpec struct {
	panic, breed, tempt, parent, stroll float64
	tempting                            func(item.Stack) bool
}

// registerAnimalGoals registers the goals of an animal at the priorities
// most animals use, with extra a goal of the species ranked before the
// stroll, if any.
func registerAnimalGoals(m *Mob, a *animalAI, spec animalSpec, extra Goal) {
	ai := m.AI
	a.panic = NewPanicGoal(spec.panic)
	priority := 0
	add := func(g Goal) {
		ai.Goals.Add(priority, g)
		priority++
	}
	add(NewFloatGoal(m))
	add(a.panic)
	add(NewBreedGoal(spec.breed))
	add(NewTemptGoal(spec.tempt, spec.tempting, false))
	add(NewFollowParentGoal(spec.parent))
	if extra != nil {
		add(extra)
	}
	add(NewWaterAvoidingRandomStrollGoal(spec.stroll))
	add(NewLookAtPlayerGoal(6))
	add(NewRandomLookAroundGoal())
}

// animalEyeHeight returns the eye height of an animal, half as high for a
// baby as AgeableMob scales it.
func animalEyeHeight(adult float64, baby bool) float64 {
	if baby {
		return adult / 2
	}
	return adult
}

// PanicGoal makes a hurt mob run to a random spot nearby, or to water when
// it burns, ported from Java's PanicGoal.
type PanicGoal struct {
	goalFlags
	SpeedModifier float64
	// Causes selects the damage the mob panics from, panicCause if nil.
	Causes func(world.DamageSource) bool

	wanted  mgl64.Vec3
	running bool
}

// NewPanicGoal returns a PanicGoal.
func NewPanicGoal(speedModifier float64) *PanicGoal {
	return &PanicGoal{goalFlags: goalFlags{GoalMove}, SpeedModifier: speedModifier}
}

func (g *PanicGoal) shouldPanic(m *Mob) bool {
	src := m.LastDamage()
	if src == nil {
		return false
	}
	if g.Causes != nil {
		return g.Causes(src)
	}
	return panicCause(src)
}

func (g *PanicGoal) CanUse(m *Mob) bool {
	if !g.shouldPanic(m) {
		return false
	}
	if m.OnFireDuration() > 0 {
		if water, ok := panicWater(m.tx, m.BlockPos(), 5); ok {
			g.wanted = water.Vec3()
			return true
		}
	}
	pos, ok := DefaultRandomPos(m, 5, 4)
	if ok {
		g.wanted = pos
	}
	return ok
}

func (g *PanicGoal) CanContinueToUse(m *Mob) bool { return !m.AI.nav.IsDone() }

func (g *PanicGoal) Start(m *Mob) {
	m.AI.nav.MoveToPos(m, g.wanted[0], g.wanted[1], g.wanted[2], g.SpeedModifier)
	g.running = true
}

func (g *PanicGoal) Stop(*Mob) { g.running = false }

// BreedGoal walks an animal in love to a partner in love nearby and breeds
// with it once both spent three seconds together, ported from Java's
// BreedGoal.
type BreedGoal struct {
	goalFlags
	SpeedModifier float64

	partner  *world.EntityHandle
	loveTime int
}

// NewBreedGoal returns a BreedGoal.
func NewBreedGoal(speedModifier float64) *BreedGoal {
	return &BreedGoal{goalFlags: goalFlags{GoalMove | GoalLook}, SpeedModifier: speedModifier}
}

func (g *BreedGoal) self(m *Mob) breeder { return m.Self.(breeder) }

func (g *BreedGoal) partnerOf(m *Mob) breeder {
	if g.partner == nil {
		return nil
	}
	e, ok := g.partner.Entity(m.tx)
	if !ok {
		return nil
	}
	p, ok := e.(breeder)
	if !ok || p.Dead() {
		return nil
	}
	return p
}

func (g *BreedGoal) CanUse(m *Mob) bool {
	a := g.self(m)
	if !a.InLove() {
		return false
	}
	p := freePartner(a, m.tx)
	if p == nil {
		return false
	}
	if s, ok := a.(breedGoalStarter); ok && !s.canStartBreeding(m.tx) {
		return false
	}
	g.partner = p.H()
	return true
}

func (g *BreedGoal) CanContinueToUse(m *Mob) bool {
	p := g.partnerOf(m)
	return p != nil && p.InLove() && g.loveTime < 60 && !panicking(p)
}

func (g *BreedGoal) Stop(*Mob) { g.partner, g.loveTime = nil, 0 }

func (g *BreedGoal) Tick(m *Mob) {
	p := g.partnerOf(m)
	if p == nil {
		return
	}
	m.AI.look.SetLookAtEntityWith(p, 10, m.AI.MaxHeadPitch)
	m.AI.nav.MoveToEntity(m, p, g.SpeedModifier)
	g.loveTime++
	if g.loveTime >= adjustedTickDelay(g, 60) && m.DistanceToSqr(p) < 9 {
		breed(g.self(m), p, m.tx)
	}
}

// TemptGoal makes a mob follow a player holding food it likes, ported from
// Java's TemptGoal.
type TemptGoal struct {
	goalFlags
	SpeedModifier float64
	Items         func(item.Stack) bool
	// CanScare makes the mob stop following a player that moves while
	// close, as cats and ocelots do. Scares, if set, decides it in its
	// place, as goals overriding TemptGoal.canScare do.
	CanScare bool
	Scares   func(m *Mob) bool
	// StopDistance is how close the mob gets, 2.5 blocks by default.
	StopDistance float64

	player                   *world.EntityHandle
	calmDown                 int
	px, py, pz, pRotX, pRotY float64
	running                  bool
}

// NewTemptGoal returns a TemptGoal for items.
func NewTemptGoal(speedModifier float64, items func(item.Stack) bool, canScare bool) *TemptGoal {
	return &TemptGoal{goalFlags: goalFlags{GoalMove | GoalLook}, SpeedModifier: speedModifier, Items: items, CanScare: canScare, StopDistance: 2.5}
}

// Running reports if the goal runs.
func (g *TemptGoal) Running() bool { return g.running }

func (g *TemptGoal) holdsItem(_ *Mob, p world.Entity) bool {
	h, ok := p.(interface {
		HeldItems() (item.Stack, item.Stack)
	})
	if !ok {
		return false
	}
	main, off := h.HeldItems()
	return g.Items(main) || g.Items(off)
}

func (g *TemptGoal) CanUse(m *Mob) bool {
	if g.calmDown > 0 {
		g.calmDown--
		return false
	}
	c := forNonCombat()
	// TEMPT_RANGE of 10.
	c.rangeLimit, c.ignoreSight, c.selector = 10, true, g.holdsItem
	p := nearestPlayer(m, c)
	if p == nil {
		g.player = nil
		return false
	}
	g.player = p.H()
	return true
}

func (g *TemptGoal) canScare(m *Mob) bool {
	if g.Scares != nil {
		return g.Scares(m)
	}
	return g.CanScare
}

func (g *TemptGoal) CanContinueToUse(m *Mob) bool {
	if g.canScare(m) && g.player != nil {
		if p, ok := g.player.Entity(m.tx); ok {
			pos, rot := p.Position(), p.Rotation()
			if m.DistanceToSqr(p) < 36 {
				if pos.Sub(mgl64.Vec3{g.px, g.py, g.pz}).LenSqr() > .010000000000000002 {
					return false
				}
				if math.Abs(rot.Pitch()-g.pRotX) > 5 || math.Abs(rot.Yaw()-g.pRotY) > 5 {
					return false
				}
			} else {
				g.px, g.py, g.pz = pos[0], pos[1], pos[2]
			}
			g.pRotX, g.pRotY = rot.Pitch(), rot.Yaw()
		}
	}
	return g.CanUse(m)
}

func (g *TemptGoal) Start(m *Mob) {
	if p, ok := g.player.Entity(m.tx); ok {
		pos := p.Position()
		g.px, g.py, g.pz = pos[0], pos[1], pos[2]
	}
	g.running = true
}

func (g *TemptGoal) Stop(m *Mob) {
	g.player = nil
	m.AI.nav.Stop()
	g.calmDown = reducedTickDelay(100)
	g.running = false
}

func (g *TemptGoal) Tick(m *Mob) {
	p, ok := g.player.Entity(m.tx)
	if !ok {
		return
	}
	m.AI.look.SetLookAtEntityWith(p, m.AI.MaxHeadYaw+20, m.AI.MaxHeadPitch)
	if m.DistanceToSqr(p) < g.StopDistance*g.StopDistance {
		m.AI.nav.Stop()
	} else {
		m.AI.nav.MoveToEntity(m, p, g.SpeedModifier)
	}
}

// FollowParentGoal makes a baby animal follow the nearest adult of its kind
// that is more than three blocks away, ported from Java's FollowParentGoal.
type FollowParentGoal struct {
	goalFlags
	SpeedModifier float64
	// MinDist and MaxDist are the distances to the adult between which the
	// baby follows it, 3 and 16 blocks as in FollowParentGoal by default.
	MinDist, MaxDist float64

	parent           *world.EntityHandle
	timeToRecalcPath int
}

// NewFollowParentGoal returns a FollowParentGoal.
func NewFollowParentGoal(speedModifier float64) *FollowParentGoal {
	return &FollowParentGoal{SpeedModifier: speedModifier, MinDist: 3, MaxDist: 16}
}

// ageable is implemented by mobs that grow up, as Java's AgeableMob.
type ageable interface{ ageTicks() *int }

func (g *FollowParentGoal) baby(m *Mob) bool { return *m.Self.(ageable).ageTicks() < 0 }

func (g *FollowParentGoal) CanUse(m *Mob) bool {
	if !g.baby(m) {
		return false
	}
	var closest world.Entity
	closestDist := math.MaxFloat64
	search := mgl64.Vec3{8, 4, 8}
	if g.MaxDist > 16 {
		// BabyFollowAdult follows the nearest adult its sensor sees.
		search = mgl64.Vec3{g.MaxDist - 1, g.MaxDist - 1, g.MaxDist - 1}
	}
	for e := range m.tx.EntitiesWithin(m.BBox().GrowVec3(search)) {
		p, ok := e.(ageable)
		if !ok || e.H().Type() != m.H().Type() || *p.ageTicks() < 0 {
			continue
		}
		if d := m.DistanceToSqr(e); d <= closestDist {
			closest, closestDist = e, d
		}
	}
	if closest == nil || closestDist < g.MinDist*g.MinDist {
		return false
	}
	g.parent = closest.H()
	return true
}

func (g *FollowParentGoal) CanContinueToUse(m *Mob) bool {
	if !g.baby(m) {
		return false
	}
	p, ok := g.parent.Entity(m.tx)
	if !ok || !entityAlive(p) {
		return false
	}
	d := m.DistanceToSqr(p)
	return d >= g.MinDist*g.MinDist && d <= g.MaxDist*g.MaxDist
}

func (g *FollowParentGoal) Start(*Mob) { g.timeToRecalcPath = 0 }

func (g *FollowParentGoal) Stop(*Mob) { g.parent = nil }

func (g *FollowParentGoal) Tick(m *Mob) {
	if g.timeToRecalcPath--; g.timeToRecalcPath > 0 {
		return
	}
	g.timeToRecalcPath = adjustedTickDelay(g, 10)
	if p, ok := g.parent.Entity(m.tx); ok {
		m.AI.nav.MoveToEntity(m, p, g.SpeedModifier)
	}
}

// EatBlockGoal makes a mob lower its head now and then and eat the grass
// it stands in or on, ported from Java's EatBlockGoal.
type EatBlockGoal struct {
	goalFlags
	// Ate is called when the mob ate, as Mob.ate.
	Ate func(m *Mob)

	eatAnimationTick int
}

// NewEatBlockGoal returns an EatBlockGoal.
func NewEatBlockGoal(ate func(m *Mob)) *EatBlockGoal {
	return &EatBlockGoal{goalFlags: goalFlags{GoalMove | GoalLook | GoalJump}, Ate: ate}
}

// Eating reports if the mob lowers its head to eat.
func (g *EatBlockGoal) Eating() bool { return g.eatAnimationTick > 0 }

func (g *EatBlockGoal) CanUse(m *Mob) bool {
	chance := 1000
	if b, ok := m.Self.(breeder); ok && *b.ageTicks() < 0 {
		chance = 50
	}
	if m.Random().NextInt(int32(adjustedTickDelay(g, chance))) != 0 {
		return false
	}
	pos := m.BlockPos()
	return blockInTag(m.tx.Block(pos), "minecraft:edible_for_sheep") || sheepGrass(m.tx, pos.Side(cube.FaceDown))
}

func (g *EatBlockGoal) Start(m *Mob) {
	g.eatAnimationTick = adjustedTickDelay(g, 40)
	if v, ok := m.Self.(interface{ viewAction(world.EntityAction) }); ok {
		v.viewAction(EatGrassAction{})
	}
	m.AI.nav.Stop()
}

func (g *EatBlockGoal) Stop(*Mob) { g.eatAnimationTick = 0 }

func (g *EatBlockGoal) CanContinueToUse(*Mob) bool { return g.eatAnimationTick > 0 }

func (g *EatBlockGoal) Tick(m *Mob) {
	g.eatAnimationTick = max(0, g.eatAnimationTick-1)
	if g.eatAnimationTick != adjustedTickDelay(g, 4) {
		return
	}
	pos := m.BlockPos()
	griefing := !mobGriefingDisabled(m.tx)
	switch {
	case blockInTag(m.tx.Block(pos), "minecraft:edible_for_sheep"):
		if griefing {
			eaten := m.tx.Block(pos)
			m.tx.SetBlock(pos, nil, nil)
			m.tx.AddParticle(pos.Vec3Centre(), particle.BlockBreak{Block: eaten})
		}
	case sheepGrass(m.tx, pos.Side(cube.FaceDown)):
		if griefing {
			below := pos.Side(cube.FaceDown)
			m.tx.AddParticle(below.Vec3Centre(), particle.BlockBreak{Block: m.tx.Block(below)})
			m.tx.SetBlock(below, block.Dirt{}, nil)
		}
	default:
		return
	}
	g.Ate(m)
}
