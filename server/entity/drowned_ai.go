package entity

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports the goals of Java's Drowned, a zombie that swims.

func (b *oceanBehaviour) mobAI() *MobAI { return &b.ai }

func drownedOf(m *Mob) *oceanBehaviour { return m.Behaviour().(*oceanBehaviour) }

// aiTickDrowned runs the MobAI of a drowned and what Zombie.aiStep does
// around it: a drowned burns in the sun without a helmet.
func (b *oceanBehaviour) aiTickDrowned(e *Ent, tx *world.Tx) *Movement {
	ai := &b.ai
	ai.MovementSpeed, ai.FollowRange, ai.StepHeight = b.speed, b.followRange, 1
	ai.EyeHeight = 1.74
	if b.baby {
		ai.EyeHeight = .775
	}
	self := &Drowned{&oceanMob{Ent: e}}
	if sunBurnTick(e, tx) {
		if b.head.Empty() {
			if e.OnFireDuration() < 8*time.Second {
				e.SetOnFire(8 * time.Second)
			}
		} else if _, durable := b.head.Item().(item.Durable); durable {
			b.head = b.head.Damage(rand.IntN(2))
		}
	}
	mov := ai.Tick(e, self, b.registerDrownedGoals)
	if !b.dead {
		b.tickDrownedSwimming(ai.Mob(e, self))
	}
	return mov
}

// doHurtTarget ports Mob.doHurtTarget for the ocean mobs, and
// Zombie.doHurtTarget for a drowned: the hit of its weapon, and a burning
// drowned without one sets its target alight.
func (b *oceanBehaviour) doHurtTarget(m *Mob, target world.Entity) bool {
	l, ok := target.(Living)
	if !ok {
		return false
	}
	damage := 3.0
	switch b.kind {
	case oceanGuardian:
		damage = 6
	case oceanElderGuardian:
		damage = 8
	}
	if w, ok := b.mainHand.Item().(item.Weapon); ok {
		damage += w.AttackDamage() - 1
	}
	if _, hurt := l.Hurt(damage, AttackDamageSource{Attacker: m.Self}); !hurt {
		return false
	}
	l.KnockBack(m.data.Pos, .4, .4)
	if b.kind != oceanDrowned {
		return true
	}
	difficulty := float64(m.tx.LocalDifficulty(m.BlockPos()).Effective)
	if b.mainHand.Empty() && m.OnFireDuration() > 0 && float64(m.Random().NextFloat()) < difficulty*.3 {
		if f, ok := target.(interface{ SetOnFire(time.Duration) }); ok {
			f.SetOnFire(time.Duration(2*int(difficulty)) * time.Second)
		}
	}
	return true
}

// registerDrownedGoals ports Zombie.registerGoals with the behaviour goals
// Drowned adds instead of those of a zombie.
func (b *oceanBehaviour) registerDrownedGoals(m *Mob) {
	ai := m.AI
	ai.nav.SetAmphibious(true)
	ai.moveOverride = &drownedMoveControl{}
	ai.SetPathfindingMalus(PathWater, 0)
	ai.Goals.Add(8, NewLookAtPlayerGoal(8))
	ai.Goals.Add(8, NewRandomLookAroundGoal())
	ai.Goals.Add(1, &drownedGoToWaterGoal{goalFlags: goalFlags{GoalMove}})
	ai.Goals.Add(2, newDrownedTridentAttackGoal())
	attack := NewMeleeAttackGoal(1, false)
	raiseArmTicks := 0
	attack.OnTick = func(m *Mob, g *MeleeAttackGoal) {
		raiseArmTicks++
		m.SetAggressive(raiseArmTicks >= 5 && g.TicksUntilNextAttack() < g.AttackInterval()/2)
	}
	ai.Goals.Add(2, &drownedAttackGoal{zombieAttackGoal{MeleeAttackGoal: attack, raiseArmTicks: &raiseArmTicks}})
	ai.Goals.Add(5, &drownedGoToBeachGoal{goalFlags: goalFlags{GoalMove | GoalJump}})
	ai.Goals.Add(6, &drownedSwimUpGoal{})
	ai.Goals.Add(7, NewRandomStrollGoal(1))
	hurt := NewHurtByTargetGoal()
	hurt.IgnoreDamageFrom = entityIs("minecraft:drowned")
	hurt.AlertOthers, hurt.AlertIgnore = true, entityIs("minecraft:zombified_piglin")
	ai.Targets.Add(1, hurt)
	players := NewNearestAttackableTargetGoal(nil, true)
	players.Selector = func(m *Mob, target world.Entity) bool { return drownedOkTarget(m, target) }
	ai.Targets.Add(2, players)
	ai.Targets.Add(3, NewNearestAttackableTargetGoal(isVillagerEntity, false))
	ai.Targets.Add(3, NewNearestAttackableTargetGoal(entityIs("minecraft:iron_golem"), true))
	ai.Targets.Add(3, NewNearestAttackableTargetGoal(entityIs("minecraft:axolotl"), true))
	turtles := NewNearestAttackableTargetGoal(entityIs("minecraft:turtle"), true)
	turtles.Selector = func(m *Mob, target world.Entity) bool { return mobIsBaby(target) && !entityInWater(m.tx, target) }
	ai.Targets.Add(5, turtles)
}

// drownedOkTarget ports Drowned.okTarget: by day a drowned only hunts what
// is in the water.
func drownedOkTarget(m *Mob, target world.Entity) bool {
	return target != nil && (!brightOutside(m.tx) || entityInWater(m.tx, target))
}

// entityInWater reports if water touches the box of an entity.
func entityInWater(tx *world.Tx, e world.Entity) bool {
	if om, ok := mobOf(e); ok && om.AI.started {
		return om.InWater()
	}
	box := e.H().Type().BBox(e).Translate(e.Position()).Grow(-.001)
	for x := int(math.Floor(box.Min()[0])); x <= int(math.Floor(box.Max()[0])); x++ {
		for y := int(math.Floor(box.Min()[1])); y <= int(math.Floor(box.Max()[1])); y++ {
			for z := int(math.Floor(box.Min()[2])); z <= int(math.Floor(box.Max()[2])); z++ {
				if pathWater(tx, cube.Pos{x, y, z}) {
					return true
				}
			}
		}
	}
	return false
}

// drownedWantsToSwim ports Drowned.wantsToSwim: a drowned swims while it
// looks for land or chases something in the water.
func (b *oceanBehaviour) drownedWantsToSwim(m *Mob) bool {
	if b.searchingForLand {
		return true
	}
	t := m.Target()
	return t != nil && entityInWater(m.tx, t)
}

// drownedUnderWater reports if the eyes of a drowned are in water, as
// Entity.isUnderWater.
func drownedUnderWater(m *Mob) bool {
	return m.AI.waterHeight > m.EyeY()-m.data.Pos[1]
}

// tickDrownedSwimming ports Drowned.updateSwimming: clients see a drowned
// swim while it wants to under water.
func (b *oceanBehaviour) tickDrownedSwimming(m *Mob) {
	swimming := drownedUnderWater(m) && b.drownedWantsToSwim(m)
	if swimming != b.swimming {
		b.swimming = swimming
		m.updateState()
	}
}

// Swimming reports if the drowned swims.
func (m *Drowned) Swimming() bool { return m.b().swimming }

// UsingItem reports if the drowned raises its trident to throw it.
func (m *Drowned) UsingItem() bool { return m.b().drawing }

// travel ports Drowned.travelInWater: a drowned swimming under water
// moves slower and keeps its speed better.
func (b *oceanBehaviour) travel(m *Mob, input mgl64.Vec3) bool {
	if b.kind == oceanGuardian || b.kind == oceanElderGuardian {
		return b.guardianTravel(m, input)
	}
	if b.kind != oceanDrowned || !m.InWater() || !drownedUnderWater(m) || !b.drownedWantsToSwim(m) {
		return false
	}
	m.AI.moveRelative(m, .01, input)
	m.AI.moveSelf(m, m.data.Vel)
	m.data.Vel = m.data.Vel.Mul(.9)
	return true
}

// drownedMoveControl ports Drowned.DrownedMoveControl: a swimming drowned
// pushes itself towards where its path leads, and otherwise walks and sinks.
type drownedMoveControl struct{}

func (c *drownedMoveControl) tick(m *Mob) {
	b := drownedOf(m)
	mc := &m.AI.move
	if !b.drownedWantsToSwim(m) || !m.InWater() {
		if !m.OnGround() {
			m.data.Vel[1] -= .008
		}
		mc.tick(m)
		return
	}
	if target := m.Target(); target != nil && target.Position()[1] > m.data.Pos[1] || b.searchingForLand {
		m.data.Vel[1] += .002
	}
	if !mc.HasWanted() || m.AI.nav.IsDone() {
		m.SetSpeed(0)
		return
	}
	wanted := mc.Wanted()
	xd, yd, zd := wanted[0]-m.data.Pos[0], wanted[1]-m.data.Pos[1], wanted[2]-m.data.Pos[2]
	dd := math.Sqrt(xd*xd + yd*yd + zd*zd)
	yd /= dd
	yaw := moveRotlerp(m.Yaw(), math.Atan2(zd, xd)*180/math.Pi-90, 90)
	m.SetYaw(yaw)
	m.AI.bodyYaw = yaw
	speed := m.AI.speed + (mc.SpeedModifier()*m.AI.MovementSpeed-m.AI.speed)*.125
	m.SetSpeed(speed)
	m.data.Vel = m.data.Vel.Add(mgl64.Vec3{speed * xd * .005, speed * yd * .1, speed * zd * .005})
}

// drownedAttackGoal ports Drowned.DrownedAttackGoal: a zombie attack on
// targets the drowned is fine hunting.
type drownedAttackGoal struct{ zombieAttackGoal }

func (g *drownedAttackGoal) CanUse(m *Mob) bool {
	return g.zombieAttackGoal.CanUse(m) && drownedOkTarget(m, m.Target())
}

func (g *drownedAttackGoal) CanContinueToUse(m *Mob) bool {
	return g.zombieAttackGoal.CanContinueToUse(m) && drownedOkTarget(m, m.Target())
}

// drownedGoToWaterGoal ports Drowned.DrownedGoToWaterGoal: by day a drowned
// on land heads for water.
type drownedGoToWaterGoal struct {
	goalFlags
	wanted mgl64.Vec3
}

func (g *drownedGoToWaterGoal) CanUse(m *Mob) bool {
	if !brightOutside(m.tx) || m.InWater() {
		return false
	}
	r := m.Random()
	origin := m.BlockPos()
	for range 10 {
		pos := origin.Add(cube.Pos{int(r.NextInt(20)) - 10, 2 - int(r.NextInt(8)), int(r.NextInt(20)) - 10})
		if _, water := m.tx.Block(pos).(block.Water); water {
			g.wanted = mgl64.Vec3{float64(pos[0]) + .5, float64(pos[1]), float64(pos[2]) + .5}
			return true
		}
	}
	return false
}

func (g *drownedGoToWaterGoal) CanContinueToUse(m *Mob) bool { return !m.AI.nav.IsDone() }

func (g *drownedGoToWaterGoal) Start(m *Mob) {
	m.AI.nav.MoveToPos(m, g.wanted[0], g.wanted[1], g.wanted[2], 1)
}

// drownedGoToBeachGoal ports Drowned.DrownedGoToBeachGoal, a MoveToBlockGoal:
// at night a drowned near the surface swims to a shore it can stand on.
type drownedGoToBeachGoal struct {
	goalFlags
	moveToBlock
}

func (g *drownedGoToBeachGoal) CanUse(m *Mob) bool {
	return g.moveToBlock.canUse(m, 8, 2, g.validTarget) && !brightOutside(m.tx) && m.InWater() && m.data.Pos[1] >= drownedSeaLevel(m)-3
}

func (g *drownedGoToBeachGoal) CanContinueToUse(m *Mob) bool {
	return g.moveToBlock.canContinueToUse(m, g.validTarget)
}

func (g *drownedGoToBeachGoal) validTarget(m *Mob, pos cube.Pos) bool {
	above := pos.Side(cube.FaceUp)
	if !isAir(m.tx.Block(above)) || !isAir(m.tx.Block(above.Side(cube.FaceUp))) {
		return false
	}
	b := m.tx.Block(pos)
	_, top := collisionTop(m.tx, pos, b)
	return top && b.Model().FaceSolid(pos, cube.FaceUp, m.tx)
}

func (g *drownedGoToBeachGoal) Start(m *Mob) {
	drownedOf(m).searchingForLand = false
	g.moveToBlock.start(m, 1)
}

func (g *drownedGoToBeachGoal) RequiresUpdateEveryTick() bool { return true }

func (g *drownedGoToBeachGoal) Tick(m *Mob) { g.moveToBlock.tick(m, 1) }

// drownedSwimUpGoal ports Drowned.DrownedSwimUpGoal: at night a drowned deep
// in the water swims up towards the surface.
type drownedSwimUpGoal struct {
	goalFlags
	stuck bool
}

func drownedSeaLevel(m *Mob) float64 { return float64(seaLevel(m.tx)) }

// seaLevel ports Level.getSeaLevel: the sea level of the noise settings of
// the dimension.
func seaLevel(tx *world.Tx) int {
	switch tx.World().Dimension() {
	case world.Nether:
		return 32
	case world.End:
		return 0
	}
	return 63
}

func (g *drownedSwimUpGoal) CanUse(m *Mob) bool {
	return !brightOutside(m.tx) && m.InWater() && m.data.Pos[1] < drownedSeaLevel(m)-2
}

func (g *drownedSwimUpGoal) CanContinueToUse(m *Mob) bool { return g.CanUse(m) && !g.stuck }

func (g *drownedSwimUpGoal) Tick(m *Mob) {
	if m.data.Pos[1] >= drownedSeaLevel(m)-1 || !(m.AI.nav.IsDone() || drownedCloseToNextPos(m)) {
		return
	}
	pos, ok := DefaultRandomPosTowards(m, 4, 8, mgl64.Vec3{m.data.Pos[0], drownedSeaLevel(m) - 1, m.data.Pos[2]}, math.Pi/2)
	if !ok {
		g.stuck = true
		return
	}
	m.AI.nav.MoveToPos(m, pos[0], pos[1], pos[2], 1)
}

func (g *drownedSwimUpGoal) Start(m *Mob) {
	drownedOf(m).searchingForLand = true
	g.stuck = false
}

func (g *drownedSwimUpGoal) Stop(m *Mob) { drownedOf(m).searchingForLand = false }

// drownedCloseToNextPos ports Drowned.closeToNextPos: the drowned is within
// two blocks of the end of its path.
func drownedCloseToNextPos(m *Mob) bool {
	target, ok := m.AI.nav.TargetPos()
	if !ok {
		return false
	}
	d := m.data.Pos.Sub(target.Vec3())
	return d.LenSqr() < 4
}

// drownedTridentAttackGoal ports Drowned.DrownedTridentAttackGoal, a
// RangedAttackGoal: a drowned holding a trident throws it every two seconds
// at targets within ten blocks.
type drownedTridentAttackGoal struct{ *RangedAttackGoal }

func newDrownedTridentAttackGoal() *drownedTridentAttackGoal {
	return &drownedTridentAttackGoal{NewRangedAttackGoal(1, 40, 10, func(m *Mob, target world.Entity, _ float64) {
		drownedOf(m).throwTrident(m, target)
	})}
}

func (g *drownedTridentAttackGoal) CanUse(m *Mob) bool {
	if _, ok := drownedOf(m).mainHand.Item().(item.Trident); !ok {
		return false
	}
	return g.RangedAttackGoal.CanUse(m)
}

func (g *drownedTridentAttackGoal) Start(m *Mob) {
	m.SetAggressive(true)
	drownedOf(m).setDrawing(m, true)
}

func (g *drownedTridentAttackGoal) Stop(m *Mob) {
	g.RangedAttackGoal.Stop(m)
	drownedOf(m).setDrawing(m, false)
	m.SetAggressive(false)
}

// setDrawing sets whether the mob uses the item it holds, as clients show
// a drawn bow or a raised trident.
func (b *oceanBehaviour) setDrawing(m *Mob, drawing bool) {
	if b.drawing != drawing {
		b.drawing = drawing
		m.updateState()
	}
}

// throwTrident ports Drowned.performRangedAttack: a trident thrown at a
// power of 1.6, aimed a little above the target.
func (b *oceanBehaviour) throwTrident(m *Mob, target world.Entity) {
	stack := b.mainHand
	if _, ok := stack.Item().(item.Trident); !ok {
		stack = item.NewStack(item.Trident{}, 1)
	}
	pos := mgl64.Vec3{m.data.Pos[0], m.EyeY() - .1, m.data.Pos[2]}
	tp := target.Position()
	xd, zd := tp[0]-m.data.Pos[0], tp[2]-m.data.Pos[2]
	yd := tp[1] + target.H().Type().BBox(target).Height()/3 - pos[1] + math.Hypot(xd, zd)*.2
	vel := shootDirection(m, mgl64.Vec3{xd, yd, zd}, 1.6, rangedAttackUncertainty(m))
	m.tx.AddEntity(NewTrident(world.EntitySpawnOpts{Position: pos, Velocity: vel}, m.Self, stack, 0, 0, false))
	r := m.Random()
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.drowned.shoot", Volume: 1, Pitch: 1 / (float64(r.NextFloat())*.4 + .8)})
}

// moveToBlock ports Java's MoveToBlockGoal: a mob looks around now and then
// for a block it wants to reach and walks onto it.
type moveToBlock struct {
	nextStartTick, tryTicks, maxStayTicks int
	pos                                   cube.Pos
	reached                               bool
	// verticalStart is MoveToBlockGoal.verticalSearchStart, and startDelay
	// the fixed delay goals overriding nextStartTick wait between searches.
	verticalStart, startDelay int
	// acceptedDistance is how close the mob gets, a block if zero, and
	// recalcInterval the ticks between paths, 40 if zero, as goals
	// overriding acceptedDistance and shouldRecalculatePath change them.
	acceptedDistance float64
	recalcInterval   int
	// targetBlock walks the mob into the block found rather than onto it,
	// as goals overriding getMoveToTarget do.
	targetBlock bool
}

// canUse ports MoveToBlockGoal.canUse with the search ranges of the goal.
func (g *moveToBlock) canUse(m *Mob, searchRange, verticalRange int, valid func(*Mob, cube.Pos) bool) bool {
	if g.nextStartTick > 0 {
		g.nextStartTick--
		return false
	}
	if g.startDelay > 0 {
		g.nextStartTick = g.startDelay
	} else {
		g.nextStartTick = reducedTickDelay(200 + int(m.Random().NextInt(200)))
	}
	return g.findNearestBlock(m, searchRange, verticalRange, valid)
}

func (g *moveToBlock) canContinueToUse(m *Mob, valid func(*Mob, cube.Pos) bool) bool {
	return g.tryTicks >= -g.maxStayTicks && g.tryTicks <= 1200 && valid(m, g.pos)
}

func (g *moveToBlock) start(m *Mob, speed float64) {
	m.AI.nav.MoveToPos(m, float64(g.pos[0])+.5, float64(g.pos[1]+1), float64(g.pos[2])+.5, speed)
	g.tryTicks = 0
	r := m.Random()
	g.maxStayTicks = int(r.NextInt(r.NextInt(1200)+1200)) + 1200
}

func (g *moveToBlock) tick(m *Mob, speed float64) {
	target := g.pos.Side(cube.FaceUp)
	if g.targetBlock {
		target = g.pos
	}
	accepted, interval := g.acceptedDistance, g.recalcInterval
	if accepted == 0 {
		accepted = 1
	}
	if interval == 0 {
		interval = 40
	}
	if target.Vec3Centre().Sub(m.data.Pos).LenSqr() >= accepted*accepted {
		g.reached = false
		g.tryTicks++
		if g.tryTicks%interval == 0 {
			m.AI.nav.MoveToPos(m, float64(target[0])+.5, float64(target[1]), float64(target[2])+.5, speed)
		}
		return
	}
	g.reached = true
	g.tryTicks--
}

// findNearestBlock ports MoveToBlockGoal.findNearestBlock: the search runs
// in rings outwards from the block below the mob.
func (g *moveToBlock) findNearestBlock(m *Mob, searchRange, verticalRange int, valid func(*Mob, cube.Pos) bool) bool {
	origin := m.BlockPos()
	next := func(v int) int {
		if v > 0 {
			return -v
		}
		return 1 - v
	}
	for y := g.verticalStart; y <= verticalRange; y = next(y) {
		for r := 0; r < searchRange; r++ {
			for x := 0; x <= r; x = next(x) {
				z := 0
				if x < r && x > -r {
					z = r
				}
				for ; z <= r; z = next(z) {
					pos := origin.Add(cube.Pos{x, y - 1, z})
					if m.WithinHome(pos) && valid(m, pos) {
						g.pos = pos
						return true
					}
				}
			}
		}
	}
	return false
}
