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

// This file ports the goals and movement of Java's Turtle.

// turtleState is the state of Java's Turtle that its goals share: whether it
// heads home, where it travels to and how long it has been laying an egg.
type turtleState struct {
	goingHome     bool
	travelPos     *cube.Pos
	layingEgg     bool
	layEggCounter int
}

// homePos returns the home beach of the turtle.
func (b *oceanWildlifeBehaviour) homePos() cube.Pos { return cube.Pos{b.homeX, b.homeY, b.homeZ} }

// nearHome reports if the turtle is within distance of the centre of its
// home, as BlockPos.closerToCenterThan.
func (b *oceanWildlifeBehaviour) nearHome(m *Mob, distance float64) bool {
	return b.homePos().Vec3Centre().Sub(m.data.Pos).LenSqr() < distance*distance
}

// tickTurtle runs a tick of a turtle: its ageing and love, the scute it
// drops growing up and its MobAI.
func (b *oceanWildlifeBehaviour) tickTurtle(e *Ent, tx *world.Tx) *Movement {
	t := &Turtle{&oceanWildlife{Ent: e}}
	ai := &b.ai
	ai.MovementSpeed, ai.StepHeight, ai.EyeHeight = b.speed, 1, .34
	if b.age < 0 {
		ai.EyeHeight = .34 * .3
	}
	if !b.homeSet {
		p := cube.PosFromVec3(e.data.Pos)
		b.homeX, b.homeY, b.homeZ, b.homeSet = p.X(), p.Y(), p.Z(), true
	}
	// Turtle.ageBoundaryReached: a turtle growing up drops a scute.
	if b.age == -1 && tx.World().GameRuleBool("mob_drops") {
		dropTurtleGrowth(e, tx)
	}
	tickAgeing(t, tx)
	mv := ai.Tick(e, t, b.registerTurtleGoals)
	if shown := (turtleShownState{b.hasEgg, b.turtle.layingEgg}); shown != b.turtleShown {
		b.turtleShown = shown
		t.updateState()
	}
	return mv
}

// turtleShownState are the flags of a turtle viewers were last shown.
type turtleShownState struct{ egg, layingEgg bool }

// registerTurtleGoals ports Turtle.registerGoals and the navigation and move
// control of its constructor.
func (b *oceanWildlifeBehaviour) registerTurtleGoals(m *Mob) {
	ai := m.AI
	ai.nav.SetAmphibious(true)
	// TurtlePathNavigation: a travelling turtle swims to water, and
	// otherwise lands on any block but air.
	ai.nav.StableDestination = func(tx *world.Tx, pos cube.Pos) bool {
		if b.turtle.travelPos != nil {
			return pathWater(tx, pos)
		}
		return !isAir(pathBlockAt(tx, pos.Side(cube.FaceDown)))
	}
	ai.moveOverride = turtleMoveControl{b}
	ai.SetPathfindingMalus(PathWater, 0)
	ai.SetPathfindingMalus(PathDoorIronClosed, -1)
	ai.SetPathfindingMalus(PathDoorWoodClosed, -1)
	ai.SetPathfindingMalus(PathDoorOpen, -1)
	b.turtlePanic = NewPanicGoal(1.2)
	ai.Goals.Add(0, turtlePanicGoal{b.turtlePanic})
	ai.Goals.Add(1, NewBreedGoal(1))
	ai.Goals.Add(1, &turtleLayEggGoal{goalFlags: goalFlags{GoalMove | GoalJump}, turtle: b})
	ai.Goals.Add(2, NewTemptGoal(1.1, func(s item.Stack) bool { return itemNamed(s, "minecraft:seagrass") }, false))
	ai.Goals.Add(3, &turtleGoToWaterGoal{goalFlags: goalFlags{GoalMove | GoalJump}, turtle: b, moveToBlock: moveToBlock{verticalStart: -1, recalcInterval: 160}})
	ai.Goals.Add(4, &turtleGoHomeGoal{turtle: b})
	ai.Goals.Add(7, &turtleTravelGoal{turtle: b})
	ai.Goals.Add(8, NewLookAtPlayerGoal(8))
	stroll := NewRandomStrollGoal(1)
	stroll.Interval = 100
	ai.Goals.Add(9, turtleRandomStrollGoal{stroll, b})
}

// walkTargetValue ports Turtle.getWalkTargetValue: a turtle that is not
// heading home likes water, and every turtle sand.
func (b *oceanWildlifeBehaviour) walkTargetValue(m *Mob, pos cube.Pos) float64 {
	if b.id != "minecraft:turtle" {
		return 0
	}
	if !b.turtle.goingHome && pathWater(m.tx, pos) {
		return 10
	}
	if blockInTag(m.tx.Block(pos.Side(cube.FaceDown)), "sand") {
		return 10
	}
	return pathfindingLightCost(m.tx, pos)
}

// travelTurtle ports Turtle.travelInWater: a turtle swims and sinks slowly,
// unless it chases something or nears its home.
func (b *oceanWildlifeBehaviour) travelTurtle(m *Mob, input mgl64.Vec3) bool {
	if !m.InWater() {
		return false
	}
	m.AI.moveRelative(m, float64(float32(.1)), input)
	m.AI.moveSelf(m, m.data.Vel)
	m.data.Vel = m.data.Vel.Mul(.9)
	if m.Target() == nil && (!b.turtle.goingHome || !b.nearHome(m, 20)) {
		m.data.Vel[1] -= .005
	}
	return true
}

// afterTurtleAiStep ports what Turtle.aiStep does after the AI: a turtle
// laying an egg on sand scrapes at it.
func (b *oceanWildlifeBehaviour) afterTurtleAiStep(m *Mob) {
	s := &b.turtle
	if !s.layingEgg || s.layEggCounter < 1 || s.layEggCounter%5 != 0 {
		return
	}
	pos := m.BlockPos()
	if below := m.tx.Block(pos.Side(cube.FaceDown)); blockInTag(below, "sand") {
		m.tx.AddParticle(pos.Vec3Centre(), particle.BlockBreak{Block: below})
		m.tx.EmitGameEvent(world.GameEventEntityAction, m.data.Pos, world.GameEventContext{Source: m.Self})
	}
}

// canFallInLove ports Turtle.canFallInLove: a turtle carrying an egg does
// not fall in love.
func (t *Turtle) canFallInLove() bool { return !t.b().hasEgg }

// HasEgg reports if the turtle carries an egg to lay at its home beach.
func (t *Turtle) HasEgg() bool { return t.b().hasEgg }

// LayingEgg reports if the turtle digs to lay its egg.
func (t *Turtle) LayingEgg() bool { return t.b().turtle.layingEgg }

// turtleMoveControl ports Turtle.TurtleMoveControl: a turtle swims slowly
// far from home and as a baby, crawls on land at half its speed and rises
// and dives towards where it wants to go.
type turtleMoveControl struct{ turtle *oceanWildlifeBehaviour }

func (c turtleMoveControl) tick(m *Mob) {
	b := c.turtle
	// TurtleMoveControl.updateSpeed.
	if m.InWater() {
		m.data.Vel[1] += .005
		if !b.nearHome(m, 16) {
			m.SetSpeed(math.Max(m.AI.speed/2, float64(float32(.08))))
		}
		if b.age < 0 {
			m.SetSpeed(math.Max(m.AI.speed/3, float64(float32(.06))))
		}
	} else if m.OnGround() {
		m.SetSpeed(math.Max(m.AI.speed/2, float64(float32(.06))))
	}
	mc := &m.AI.move
	if mc.operation != moveTo || m.AI.nav.IsDone() {
		m.SetSpeed(0)
		return
	}
	xd, yd, zd := mc.wanted[0]-m.data.Pos[0], mc.wanted[1]-m.data.Pos[1], mc.wanted[2]-m.data.Pos[2]
	dd := math.Sqrt(xd*xd + yd*yd + zd*zd)
	if dd < 1e-5 {
		m.SetSpeed(0)
		return
	}
	yaw := moveRotlerp(m.Yaw(), math.Atan2(zd, xd)*180/math.Pi-90, 90)
	m.SetYaw(yaw)
	m.AI.bodyYaw = yaw
	target := mc.speedModifier * m.AI.MovementSpeed
	m.SetSpeed(m.AI.speed + (target-m.AI.speed)*float64(float32(.125)))
	m.data.Vel[1] += m.AI.speed * (yd / dd) * .1
}

// turtlePanicGoal ports Turtle.TurtlePanicGoal: a panicking turtle heads for
// water within seven blocks, or else a random spot nearby.
type turtlePanicGoal struct{ *PanicGoal }

func (g turtlePanicGoal) CanUse(m *Mob) bool {
	if !g.shouldPanic(m) {
		return false
	}
	if water, ok := panicWater(m.tx, m.BlockPos(), 7); ok {
		g.wanted = water.Vec3()
		return true
	}
	pos, ok := DefaultRandomPos(m, 5, 4)
	if ok {
		g.wanted = pos
	}
	return ok
}

// turtleLayEggGoal ports Turtle.TurtleLayEggGoal: a turtle carrying an egg
// within nine blocks of home finds sand with room above within 16 blocks
// and lays a clutch of one to four eggs after digging for ten seconds.
type turtleLayEggGoal struct {
	goalFlags
	moveToBlock
	turtle *oceanWildlifeBehaviour
}

func turtleEggSpot(m *Mob, pos cube.Pos) bool {
	return isAir(m.tx.Block(pos.Side(cube.FaceUp))) && blockInTag(m.tx.Block(pos), "sand")
}

func (g *turtleLayEggGoal) CanUse(m *Mob) bool {
	return g.turtle.hasEgg && g.turtle.nearHome(m, 9) && g.canUse(m, 16, 1, turtleEggSpot)
}

func (g *turtleLayEggGoal) CanContinueToUse(m *Mob) bool {
	return g.canContinueToUse(m, turtleEggSpot) && g.turtle.hasEgg && g.turtle.nearHome(m, 9)
}

func (g *turtleLayEggGoal) RequiresUpdateEveryTick() bool { return true }

func (g *turtleLayEggGoal) Start(m *Mob) { g.start(m, 1) }

func (g *turtleLayEggGoal) Tick(m *Mob) {
	g.tick(m, 1)
	b, s := g.turtle, &g.turtle.turtle
	if m.InWater() || !g.reached {
		return
	}
	if s.layEggCounter < 1 {
		s.layingEgg, s.layEggCounter = true, 1
	} else if s.layEggCounter > adjustedTickDelay(g, 200) {
		r := m.Random()
		m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.turtle.lay_egg", Volume: .3, Pitch: float64(.9 + r.NextFloat()*.2)})
		eggPos := g.pos.Side(cube.FaceUp)
		egg := block.TurtleEgg{Eggs: uint8(r.NextInt(4) + 1)}
		m.tx.SetBlock(eggPos, egg, nil)
		m.tx.EmitGameEvent(world.GameEventBlockPlace, eggPos.Vec3Centre(), world.GameEventContext{Source: m.Self, AffectedState: egg})
		b.hasEgg = false
		s.layingEgg, s.layEggCounter = false, 0
		b.breeding.inLove = 600
	}
	if s.layingEgg {
		s.layEggCounter++
	}
}

// turtleGoToWaterGoal ports Turtle.TurtleGoToWaterGoal: a turtle on land
// that is neither heading home nor carrying an egg, and any baby, heads for
// water within 24 blocks.
type turtleGoToWaterGoal struct {
	goalFlags
	moveToBlock
	turtle *oceanWildlifeBehaviour
}

func (g *turtleGoToWaterGoal) speed() float64 {
	if g.turtle.age < 0 {
		return 2
	}
	return 1
}

func (g *turtleGoToWaterGoal) CanUse(m *Mob) bool {
	b := g.turtle
	if b.age < 0 && !m.InWater() || !b.turtle.goingHome && !m.InWater() && !b.hasEgg {
		return g.canUse(m, 24, 1, func(m *Mob, pos cube.Pos) bool { return pathWater(m.tx, pos) })
	}
	return false
}

func (g *turtleGoToWaterGoal) CanContinueToUse(m *Mob) bool {
	return !m.InWater() && g.tryTicks <= 1200 && pathWater(m.tx, g.pos)
}

func (g *turtleGoToWaterGoal) RequiresUpdateEveryTick() bool { return true }

func (g *turtleGoToWaterGoal) Start(m *Mob) { g.start(m, g.speed()) }

func (g *turtleGoToWaterGoal) Tick(m *Mob) { g.tick(m, g.speed()) }

// turtleGoHomeGoal ports Turtle.TurtleGoHomeGoal: a turtle carrying an egg,
// or now and then one over 64 blocks from home, heads home, giving up half
// a minute after it got close.
type turtleGoHomeGoal struct {
	goalFlags
	turtle         *oceanWildlifeBehaviour
	stuck          bool
	closeHomeTicks int
}

func (g *turtleGoHomeGoal) CanUse(m *Mob) bool {
	b := g.turtle
	if b.age < 0 {
		return false
	}
	if b.hasEgg {
		return true
	}
	return m.Random().NextInt(int32(reducedTickDelay(700))) == 0 && !b.nearHome(m, 64)
}

func (g *turtleGoHomeGoal) Start(*Mob) {
	g.turtle.turtle.goingHome = true
	g.stuck, g.closeHomeTicks = false, 0
}

func (g *turtleGoHomeGoal) Stop(*Mob) { g.turtle.turtle.goingHome = false }

func (g *turtleGoHomeGoal) CanContinueToUse(m *Mob) bool {
	return !g.turtle.nearHome(m, 7) && !g.stuck && g.closeHomeTicks <= adjustedTickDelay(g, 600)
}

func (g *turtleGoHomeGoal) Tick(m *Mob) {
	b := g.turtle
	close := b.nearHome(m, 16)
	if close {
		g.closeHomeTicks++
	}
	if !m.AI.nav.IsDone() {
		return
	}
	home := b.homePos()
	homeVec := mgl64.Vec3{float64(home.X()) + .5, float64(home.Y()), float64(home.Z()) + .5}
	next, ok := DefaultRandomPosTowards(m, 16, 3, homeVec, math.Pi/10)
	if !ok {
		next, ok = DefaultRandomPosTowards(m, 8, 7, homeVec, math.Pi/2)
	}
	if ok && !close {
		if _, water := m.tx.Block(cube.PosFromVec3(next)).(block.Water); !water {
			next, ok = DefaultRandomPosTowards(m, 16, 5, homeVec, math.Pi/2)
		}
	}
	if !ok {
		g.stuck = true
		return
	}
	m.AI.nav.MoveToPos(m, next[0], next[1], next[2], 1)
}

// turtleTravelGoal ports Turtle.TurtleTravelGoal: a turtle in water swims
// off towards a spot up to 512 blocks away.
type turtleTravelGoal struct {
	goalFlags
	turtle *oceanWildlifeBehaviour
	stuck  bool
}

func (g *turtleTravelGoal) CanUse(m *Mob) bool {
	b := g.turtle
	return !b.turtle.goingHome && !b.hasEgg && m.InWater()
}

func (g *turtleTravelGoal) Start(m *Mob) {
	r := m.Random()
	xt, yt, zt := int(r.NextInt(1025))-512, int(r.NextInt(9))-4, int(r.NextInt(1025))-512
	if float64(yt)+m.data.Pos[1] > float64(seaLevel(m.tx)-1) {
		yt = 0
	}
	pos := cube.PosFromVec3(m.data.Pos.Add(mgl64.Vec3{float64(xt), float64(yt), float64(zt)}))
	g.turtle.turtle.travelPos, g.stuck = &pos, false
}

func (g *turtleTravelGoal) Tick(m *Mob) {
	s := &g.turtle.turtle
	if s.travelPos == nil {
		g.stuck = true
		return
	}
	if !m.AI.nav.IsDone() {
		return
	}
	target := mgl64.Vec3{float64(s.travelPos.X()) + .5, float64(s.travelPos.Y()), float64(s.travelPos.Z()) + .5}
	next, ok := DefaultRandomPosTowards(m, 16, 3, target, math.Pi/10)
	if !ok {
		next, ok = DefaultRandomPosTowards(m, 8, 7, target, math.Pi/2)
	}
	if ok {
		// Level.hasChunksAt: the turtle only travels where every chunk
		// within 34 blocks is loaded.
		x, z := int(math.Floor(next[0])), int(math.Floor(next[2]))
		for cx := (x - 34) >> 4; ok && cx <= (x+34)>>4; cx++ {
			for cz := (z - 34) >> 4; ok && cz <= (z+34)>>4; cz++ {
				_, ok = m.tx.BlockLoaded(cube.Pos{cx << 4, m.BlockPos().Y(), cz << 4})
			}
		}
	}
	if !ok {
		g.stuck = true
		return
	}
	m.AI.nav.MoveToPos(m, next[0], next[1], next[2], 1)
}

func (g *turtleTravelGoal) CanContinueToUse(m *Mob) bool {
	b := g.turtle
	return !m.AI.nav.IsDone() && !g.stuck && !b.turtle.goingHome && !b.breeding.InLove() && !b.hasEgg
}

func (g *turtleTravelGoal) Stop(*Mob) { g.turtle.turtle.travelPos = nil }

// turtleRandomStrollGoal ports Turtle.TurtleRandomStrollGoal, which only a
// turtle on land without an egg and not heading home runs.
type turtleRandomStrollGoal struct {
	*RandomStrollGoal
	turtle *oceanWildlifeBehaviour
}

func (g turtleRandomStrollGoal) CanUse(m *Mob) bool {
	b := g.turtle
	return !m.InWater() && !b.turtle.goingHome && !b.hasEgg && g.RandomStrollGoal.CanUse(m)
}
