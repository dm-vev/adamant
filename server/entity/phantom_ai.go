package entity

import (
	"math"
	"slices"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports the goals of Java's Phantom.

// phantomPhase is Phantom.AttackPhase.
type phantomPhase uint8

const (
	phantomCircle phantomPhase = iota
	phantomSwoop
)

func (s *phantomState) mobAI() *MobAI { return &s.ai }

func phantomOf(m *Mob) *phantomState { return m.Behaviour().(*phantomState) }

// aiTick runs the MobAI of a phantom and what Mob.aiStep does before it: a
// phantom burns in the sun.
func (s *phantomState) aiTick(e *Ent, tx *world.Tx) *Movement {
	ai := &s.ai
	ai.MovementSpeed, ai.EyeHeight = s.speed, .175*(1+.15*float64(s.size))
	if sunBurnTick(e, tx) && e.OnFireDuration() < 8*time.Second {
		e.SetOnFire(8 * time.Second)
	}
	return ai.Tick(e, &Phantom{Ent: e}, s.registerGoals)
}

// registerGoals ports Phantom.registerGoals.
func (s *phantomState) registerGoals(m *Mob) {
	ai := m.AI
	ai.moveOverride = &phantomMoveControl{speed: .1}
	ai.lookOverride = phantomLookControl{}
	ai.bodyOverride = phantomBodyControl{}
	ai.Goals.Add(1, &phantomAttackStrategyGoal{})
	ai.Goals.Add(2, &phantomSweepAttackGoal{goalFlags: goalFlags{GoalMove}})
	ai.Goals.Add(3, &phantomCircleAroundAnchorGoal{goalFlags: goalFlags{GoalMove}})
	ai.Targets.Add(1, &phantomAttackPlayerTargetGoal{nextScanTick: reducedTickDelay(20)})
}

// travel ports Phantom.travel: a phantom flies.
func (s *phantomState) travel(m *Mob, input mgl64.Vec3) bool {
	m.AI.travelFlying(m, input, .02, .02, .2)
	return true
}

// doHurtTarget ports Mob.doHurtTarget for a phantom: a bite of 6, a point
// more per size.
func (s *phantomState) doHurtTarget(m *Mob, target world.Entity) bool {
	l, ok := target.(Living)
	if !ok {
		return false
	}
	if _, hurt := l.Hurt(float64(6+s.size), AttackDamageSource{Attacker: m.Self}); !hurt {
		return false
	}
	l.KnockBack(m.data.Pos, .4, .4)
	return true
}

// phantomCanAttack ports Phantom.canAttack with TargetingConditions.DEFAULT:
// a target that may be attacked, in any light and at any distance.
func phantomCanAttack(m *Mob, target world.Entity) bool {
	c := forCombat()
	return c.test(m, target)
}

// phantomAttackPlayerTargetGoal ports Phantom.PhantomAttackPlayerTargetGoal:
// every three seconds a phantom looks for the highest player below it that
// it sees.
type phantomAttackPlayerTargetGoal struct {
	goalFlags
	nextScanTick int
}

func (g *phantomAttackPlayerTargetGoal) CanUse(m *Mob) bool {
	if g.nextScanTick > 0 {
		g.nextScanTick--
		return false
	}
	g.nextScanTick = reducedTickDelay(60)
	c := forCombat()
	c.rangeLimit = 64
	box := m.BBox().GrowVec3(mgl64.Vec3{16, 64, 16})
	var players []world.Entity
	for p := range m.tx.Players() {
		if box.Vec3Within(p.Position()) && c.test(m, p) {
			players = append(players, p)
		}
	}
	slices.SortStableFunc(players, func(a, b world.Entity) int {
		switch {
		case a.Position()[1] > b.Position()[1]:
			return -1
		case a.Position()[1] < b.Position()[1]:
			return 1
		}
		return 0
	})
	for _, p := range players {
		if phantomCanAttack(m, p) {
			m.SetTarget(p)
			return true
		}
	}
	return false
}

func (g *phantomAttackPlayerTargetGoal) CanContinueToUse(m *Mob) bool {
	t := m.Target()
	return t != nil && phantomCanAttack(m, t)
}

// phantomAttackStrategyGoal ports Phantom.PhantomAttackStrategyGoal: a
// phantom circles high above its target and swoops at it every eight to
// eleven seconds.
type phantomAttackStrategyGoal struct {
	goalFlags
	nextSweepTick int
}

func (g *phantomAttackStrategyGoal) CanUse(m *Mob) bool {
	t := m.Target()
	return t != nil && phantomCanAttack(m, t)
}

func (g *phantomAttackStrategyGoal) Start(m *Mob) {
	g.nextSweepTick = reducedTickDelay(10)
	phantomOf(m).phase = phantomCircle
	g.setAnchorAboveTarget(m)
}

func (g *phantomAttackStrategyGoal) Stop(m *Mob) {
	s := phantomOf(m)
	if !s.anchorSet {
		return
	}
	anchor := cube.PosFromVec3(s.anchor)
	anchor[1] = m.tx.JavaMotionBlockingHeight(anchor[0], anchor[2], false) + 10 + int(m.Random().NextInt(20))
	s.anchor = anchor.Vec3()
}

func (g *phantomAttackStrategyGoal) Tick(m *Mob) {
	s := phantomOf(m)
	if s.phase != phantomCircle {
		return
	}
	if g.nextSweepTick--; g.nextSweepTick <= 0 {
		s.phase = phantomSwoop
		g.setAnchorAboveTarget(m)
		r := m.Random()
		g.nextSweepTick = reducedTickDelay((8 + int(r.NextInt(4))) * 20)
		m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.phantom.swoop", Volume: 10, Pitch: .95 + float64(r.NextFloat())*.1})
	}
}

func (g *phantomAttackStrategyGoal) setAnchorAboveTarget(m *Mob) {
	s := phantomOf(m)
	target := m.Target()
	if !s.anchorSet || target == nil {
		return
	}
	anchor := cube.PosFromVec3(target.Position()).Add(cube.Pos{0, 20 + int(m.Random().NextInt(20)), 0})
	if level := seaLevel(m.tx); anchor[1] < level {
		anchor[1] = level + 1
	}
	s.anchor = anchor.Vec3()
}

// phantomSweepAttackGoal ports Phantom.PhantomSweepAttackGoal: a swooping
// phantom dives at its target and bites it, unless a cat is near.
type phantomSweepAttackGoal struct {
	goalFlags
	scaredOfCat   bool
	catSearchTick int
}

func (g *phantomSweepAttackGoal) CanUse(m *Mob) bool {
	return m.Target() != nil && phantomOf(m).phase == phantomSwoop
}

func (g *phantomSweepAttackGoal) CanContinueToUse(m *Mob) bool {
	target := m.Target()
	if target == nil || !entityAlive(target) || creativeOrSpectator(target) || !g.CanUse(m) {
		return false
	}
	if m.AI.tickCount > g.catSearchTick {
		g.catSearchTick = m.AI.tickCount + 20
		g.scaredOfCat = false
		for e := range m.tx.EntitiesWithin(m.BBox().Grow(16)) {
			if cat, ok := e.(*Cat); ok && entityAlive(e) {
				cat.Hiss(m.tx)
				g.scaredOfCat = true
			}
		}
	}
	return !g.scaredOfCat
}

func (g *phantomSweepAttackGoal) Stop(m *Mob) {
	m.SetTarget(nil)
	phantomOf(m).phase = phantomCircle
}

func (g *phantomSweepAttackGoal) Tick(m *Mob) {
	target := m.Target()
	if target == nil {
		return
	}
	s := phantomOf(m)
	tp := target.Position()
	s.moveTarget = mgl64.Vec3{tp[0], tp[1] + target.H().Type().BBox(target).Height()*.5, tp[2]}
	if m.BBox().Grow(.2).IntersectsWith(target.H().Type().BBox(target).Translate(tp)) {
		m.doHurtTarget(target)
		s.phase = phantomCircle
		m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.phantom.bite", Volume: 1, Pitch: 1})
	} else if m.AI.collidedHorizontally || m.AI.tickCount-m.AI.lastHurtByTime < 100 && m.AI.lastHurtBy != nil {
		s.phase = phantomCircle
	}
}

// phantomCircleAroundAnchorGoal ports Phantom.PhantomCircleAroundAnchorGoal:
// a phantom circles its anchor, changing its height, distance and direction
// now and then.
type phantomCircleAroundAnchorGoal struct {
	goalFlags
	angle, distance, height, clockwise float64
}

func (g *phantomCircleAroundAnchorGoal) CanUse(m *Mob) bool {
	return m.Target() == nil || phantomOf(m).phase == phantomCircle
}

func (g *phantomCircleAroundAnchorGoal) Start(m *Mob) {
	r := m.Random()
	g.distance = 5 + float64(r.NextFloat())*10
	g.height = -4 + float64(r.NextFloat())*9
	g.clockwise = 1
	if !r.NextBoolean() {
		g.clockwise = -1
	}
	g.selectNext(m)
}

func (g *phantomCircleAroundAnchorGoal) Tick(m *Mob) {
	r := m.Random()
	s := phantomOf(m)
	if r.NextInt(int32(reducedTickDelay(350))) == 0 {
		g.height = -4 + float64(r.NextFloat())*9
	}
	if r.NextInt(int32(reducedTickDelay(250))) == 0 {
		if g.distance++; g.distance > 15 {
			g.distance, g.clockwise = 5, -g.clockwise
		}
	}
	if r.NextInt(int32(reducedTickDelay(450))) == 0 {
		g.angle = float64(r.NextFloat()) * 2 * math.Pi
		g.selectNext(m)
	}
	if s.moveTarget.Sub(m.data.Pos).LenSqr() < 4 {
		g.selectNext(m)
	}
	if s.moveTarget[1] < m.data.Pos[1] && !isAir(m.tx.Block(m.BlockPos().Side(cube.FaceDown))) {
		g.height = math.Max(1, g.height)
		g.selectNext(m)
	}
	if s.moveTarget[1] > m.data.Pos[1] && !isAir(m.tx.Block(m.BlockPos().Side(cube.FaceUp))) {
		g.height = math.Min(-1, g.height)
		g.selectNext(m)
	}
}

func (g *phantomCircleAroundAnchorGoal) selectNext(m *Mob) {
	s := phantomOf(m)
	if !s.anchorSet {
		s.anchor, s.anchorSet = m.BlockPos().Vec3(), true
	}
	g.angle += g.clockwise * 15 * math.Pi / 180
	a := float64(float32(g.angle))
	s.moveTarget = s.anchor.Add(mgl64.Vec3{g.distance * math.Cos(a), -4 + g.height, g.distance * math.Sin(a)})
}

// phantomMoveControl ports Phantom.PhantomMoveControl: a phantom glides
// towards its move target, speeding up while it flies straight.
type phantomMoveControl struct{ speed float64 }

func (c *phantomMoveControl) tick(m *Mob) {
	s := phantomOf(m)
	if m.AI.collidedHorizontally {
		m.SetYaw(m.Yaw() + 180)
		c.speed = .1
	}
	tdx := s.moveTarget[0] - m.data.Pos[0]
	tdy := s.moveTarget[1] - m.data.Pos[1]
	tdz := s.moveTarget[2] - m.data.Pos[2]
	sd := math.Sqrt(tdx*tdx + tdz*tdz)
	if math.Abs(sd) <= 1e-5 {
		return
	}
	scale := 1 - math.Abs(tdy*.7)/sd
	tdx, tdz = tdx*scale, tdz*scale
	sd2 := math.Sqrt(tdx*tdx + tdz*tdz + tdy*tdy)
	prev := m.Yaw()
	angle := math.Atan2(tdz, tdx)
	a := wrapDegrees(m.Yaw() + 90)
	b := wrapDegrees(angle * 180 / math.Pi)
	m.SetYaw(approachDegrees(a, b, 4) - 90)
	m.AI.bodyYaw = m.Yaw()
	if math.Abs(wrapDegrees(m.Yaw()-prev)) < 3 {
		c.speed = approach(c.speed, 1.8, .005*(1.8/c.speed))
	} else {
		c.speed = approach(c.speed, .2, .025)
	}
	pitch := -(math.Atan2(-tdy, sd) * 180 / math.Pi)
	m.data.Rot[1] = pitch
	moveAngle := (m.Yaw() + 90) * math.Pi / 180
	txd := c.speed * math.Cos(moveAngle) * math.Abs(tdx/sd2)
	tzd := c.speed * math.Sin(moveAngle) * math.Abs(tdz/sd2)
	tyd := c.speed * math.Sin(pitch*math.Pi/180) * math.Abs(tdy/sd2)
	m.data.Vel = m.data.Vel.Add(mgl64.Vec3{txd, tyd, tzd}.Sub(m.data.Vel).Mul(.2))
}

// phantomLookControl ports Phantom.PhantomLookControl, which turns nothing.
type phantomLookControl struct{}

func (phantomLookControl) tick(*Mob) {}

// phantomBodyControl ports Phantom.PhantomBodyRotationControl: the head of
// a phantom follows its body a tick late.
type phantomBodyControl struct{}

func (phantomBodyControl) tick(m *Mob) {
	m.AI.headYaw = m.AI.bodyYaw
	m.AI.bodyYaw = m.Yaw()
}

// approach ports Mth.approach.
func approach(value, target, delta float64) float64 {
	delta = math.Abs(delta)
	if value < target {
		return math.Min(value+delta, target)
	}
	return math.Max(value-delta, target)
}

// approachDegrees ports Mth.approachDegrees.
func approachDegrees(value, target, delta float64) float64 {
	return approach(value, value+wrapDegrees(target-value), delta)
}
