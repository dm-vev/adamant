package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports the goals of Java's Cat and Ocelot.

func (b *CatBehaviour) tamableState() *tamable { return &b.tamable }

// wantsToAttack ports TamableAnimal.wantsToAttack, which cats do not
// override.
func (b *CatBehaviour) wantsToAttack(*Mob, world.Entity, world.Entity) bool { return true }

// canAttackEntity ports TamableAnimal.canAttack: a pet never attacks its
// owner.
func (b *CatBehaviour) canAttackEntity(_ *Mob, target world.Entity) bool {
	return !b.ownedBy(target)
}

// alliedTo ports TamableAnimal.considersEntityAsAlly.
func (b *CatBehaviour) alliedTo(_ *Mob, target world.Entity) bool { return b.ownedBy(target) }

func isRabbit(e world.Entity) bool { return e.H().Type() == RabbitType }

func isTurtle(e world.Entity) bool { return e.H().Type() == TurtleType }

// registerGoals ports Cat.registerGoals and the CatAvoidEntityGoal
// Cat.reassessTameGoals gives stray cats.
func (b *CatBehaviour) registerGoals(m *Mob) {
	ai := m.AI
	tempt := &catTemptGoal{TemptGoal: NewTemptGoal(.6, (&Cat{Ent: m.Ent}).breedingFood, true), cat: b}
	tempt.Scares = func(*Mob) bool { return tempt.selected == nil || tempt.selected != tempt.player }
	b.tempt = tempt
	b.panic = NewPanicGoal(1.5)
	ai.Goals.Add(1, NewFloatGoal(m))
	ai.Goals.Add(1, tamableAnimalPanicGoal{b.panic})
	ai.Goals.Add(2, newSitWhenOrderedToGoal())
	ai.Goals.Add(3, &catRelaxOnOwnerGoal{cat: b})
	ai.Goals.Add(4, tempt)
	ai.Goals.Add(5, &catLieOnBlockGoal{goalFlags: goalFlags{GoalJump | GoalMove}, moveToBlock: moveToBlock{verticalStart: -2, startDelay: 40}, cat: b})
	ai.Goals.Add(6, newFollowOwnerGoal(1, 10, 5))
	ai.Goals.Add(7, &catSitOnBlockGoal{goalFlags: goalFlags{GoalMove | GoalJump}, cat: b})
	ai.Goals.Add(8, newLeapAtTargetGoal(.3))
	ai.Goals.Add(9, newOcelotAttackGoal())
	ai.Goals.Add(10, NewBreedGoal(.8))
	stroll := NewWaterAvoidingRandomStrollGoal(.8)
	stroll.Probability = 1.0000001e-5
	ai.Goals.Add(11, stroll)
	ai.Goals.Add(12, NewLookAtPlayerGoal(10))
	ai.Goals.Add(4, felineAvoidPlayersGoal{NewAvoidEntityGoal(isPlayerEntity, 16, .8, 1.33), func() bool { return !b.tamed }})
	ai.Targets.Add(1, newNonTameRandomTargetGoal(isRabbit, false, nil))
	ai.Targets.Add(1, newNonTameRandomTargetGoal(isTurtle, false, turtleBabyOnLand))
}

// registerGoals ports Ocelot.registerGoals and the OcelotAvoidEntityGoal
// Ocelot.reassessTrustingGoals gives ocelots that do not trust players.
func (b *ocelotBehaviour) registerGoals(m *Mob) {
	ai := m.AI
	tempt := NewTemptGoal(.6, (&Ocelot{Ent: m.Ent}).breedingFood, true)
	tempt.Scares = func(*Mob) bool { return !b.trusting }
	b.tempt = tempt
	ai.Goals.Add(1, NewFloatGoal(m))
	ai.Goals.Add(3, tempt)
	ai.Goals.Add(7, newLeapAtTargetGoal(.3))
	ai.Goals.Add(8, newOcelotAttackGoal())
	ai.Goals.Add(9, NewBreedGoal(.8))
	stroll := NewWaterAvoidingRandomStrollGoal(.8)
	stroll.Probability = 1.0000001e-5
	ai.Goals.Add(10, stroll)
	ai.Goals.Add(11, NewLookAtPlayerGoal(10))
	ai.Goals.Add(4, felineAvoidPlayersGoal{NewAvoidEntityGoal(isPlayerEntity, 16, .8, 1.33), func() bool { return !b.trusting }})
	ai.Targets.Add(1, NewNearestAttackableTargetGoal(func(e world.Entity) bool { return e.H().Type() == ChickenType }, false))
	turtles := NewNearestAttackableTargetGoal(isTurtle, false)
	turtles.Selector = turtleBabyOnLand
	ai.Targets.Add(1, turtles)
}

// felinePose ports the customServerAiStep of Cat and Ocelot: a feline
// creeps while it walks at 0.6 times its speed and sprints at 1.33 times.
func felinePose(m *Mob) (crouching, sprinting bool) {
	if !m.AI.move.HasWanted() {
		return false, false
	}
	switch m.AI.move.SpeedModifier() {
	case .6:
		return true, false
	case 1.33:
		return false, true
	}
	return false, false
}

// doHurtTarget ports Mob.doHurtTarget for felines, which scratch for 3
// damage.
func felineHurtTarget(m *Mob, target world.Entity) bool {
	l, ok := target.(Living)
	if !ok {
		return false
	}
	if _, hurt := l.Hurt(3, AttackDamageSource{Attacker: m.Self}); !hurt {
		return false
	}
	l.KnockBack(m.data.Pos, .4, .4)
	return true
}

func (b *CatBehaviour) doHurtTarget(m *Mob, target world.Entity) bool {
	return felineHurtTarget(m, target)
}

func (b *ocelotBehaviour) doHurtTarget(m *Mob, target world.Entity) bool {
	return felineHurtTarget(m, target)
}

func (b *CatBehaviour) customServerAiStep(m *Mob) { b.crouching, b.sprinting = felinePose(m) }

func (b *ocelotBehaviour) customServerAiStep(m *Mob) { b.crouching, b.sprinting = felinePose(m) }

// felineAvoidPlayersGoal ports CatAvoidEntityGoal and OcelotAvoidEntityGoal:
// a stray cat or an ocelot that does not trust players flees from them.
type felineAvoidPlayersGoal struct {
	*AvoidEntityGoal
	wild func() bool
}

func (g felineAvoidPlayersGoal) CanUse(m *Mob) bool { return g.wild() && g.AvoidEntityGoal.CanUse(m) }

func (g felineAvoidPlayersGoal) CanContinueToUse(m *Mob) bool {
	return g.wild() && g.AvoidEntityGoal.CanContinueToUse(m)
}

// catTemptGoal ports Cat.CatTemptGoal: a stray cat follows a player holding
// fish, now and then settling on one it no longer flees from when it moves.
type catTemptGoal struct {
	*TemptGoal
	cat      *CatBehaviour
	selected *world.EntityHandle
}

func (g *catTemptGoal) CanUse(m *Mob) bool { return g.TemptGoal.CanUse(m) && !g.cat.tamed }

func (g *catTemptGoal) Tick(m *Mob) {
	g.TemptGoal.Tick(m)
	r := m.Random()
	if g.selected == nil && r.NextInt(int32(adjustedTickDelay(g, 600))) == 0 {
		g.selected = g.player
	} else if r.NextInt(int32(adjustedTickDelay(g, 500))) == 0 {
		g.selected = nil
	}
}

// ocelotAttackGoal ports OcelotAttackGoal: a feline stalks its target at
// 0.6 times its speed, sprints at it from four blocks away and scratches it
// once a second when in reach.
type ocelotAttackGoal struct {
	goalFlags
	target     *world.EntityHandle
	attackTime int
}

func newOcelotAttackGoal() *ocelotAttackGoal {
	return &ocelotAttackGoal{goalFlags: goalFlags{GoalMove | GoalLook}}
}

func (g *ocelotAttackGoal) CanUse(m *Mob) bool {
	target := m.Target()
	if target == nil {
		return false
	}
	g.target = target.H()
	return true
}

func (g *ocelotAttackGoal) CanContinueToUse(m *Mob) bool {
	target, ok := g.target.Entity(m.tx)
	if !ok || !entityAlive(target) || m.DistanceToSqr(target) > 225 {
		return false
	}
	return !m.AI.nav.IsDone() || g.CanUse(m)
}

func (g *ocelotAttackGoal) Stop(m *Mob) {
	g.target = nil
	m.AI.nav.Stop()
}

func (g *ocelotAttackGoal) RequiresUpdateEveryTick() bool { return true }

func (g *ocelotAttackGoal) Tick(m *Mob) {
	target, ok := g.target.Entity(m.tx)
	if !ok {
		return
	}
	m.AI.look.SetLookAtEntityWith(target, 30, 30)
	reach := m.Width() * 2
	reachSqr := reach * reach
	distSqr := m.DistanceToSqr(target)
	speed := .8
	if distSqr > reachSqr && distSqr < 16 {
		speed = 1.33
	} else if distSqr < 225 {
		speed = .6
	}
	m.AI.nav.MoveToEntity(m, target, speed)
	g.attackTime = max(g.attackTime-1, 0)
	if distSqr <= reachSqr && g.attackTime <= 0 {
		g.attackTime = 20
		m.doHurtTarget(target)
	}
}

// catLieOnBlockGoal ports CatLieOnBlockGoal: a tamed cat now and then walks
// to a bed or furnace within eight blocks and lies down on it.
type catLieOnBlockGoal struct {
	goalFlags
	moveToBlock
	cat *CatBehaviour
}

func (g *catLieOnBlockGoal) valid(m *Mob, pos cube.Pos) bool {
	_, air := m.tx.Block(pos.Side(cube.FaceUp)).(block.Air)
	return air && blockInTag(m.tx.Block(pos), "minecraft:cats_can_lie_on")
}

func (g *catLieOnBlockGoal) CanUse(m *Mob) bool {
	c := g.cat
	return c.tamed && !c.orderedToSit && !c.lying && g.moveToBlock.canUse(m, 8, 6, g.valid)
}

func (g *catLieOnBlockGoal) CanContinueToUse(m *Mob) bool {
	return g.moveToBlock.canContinueToUse(m, g.valid)
}

func (g *catLieOnBlockGoal) RequiresUpdateEveryTick() bool { return true }

func (g *catLieOnBlockGoal) Start(m *Mob) {
	g.moveToBlock.start(m, 1.1)
	g.cat.inSittingPose = false
}

func (g *catLieOnBlockGoal) Stop(*Mob) { g.cat.lying = false }

func (g *catLieOnBlockGoal) Tick(m *Mob) {
	g.moveToBlock.tick(m, 1.1)
	g.cat.inSittingPose = false
	g.cat.lying = g.reached
}

// catSitOnBlockGoal ports CatSitOnBlockGoal: a tamed cat now and then sits
// down on a closed chest, a lit furnace or the foot of a bed.
type catSitOnBlockGoal struct {
	goalFlags
	moveToBlock
	cat *CatBehaviour
}

func (g *catSitOnBlockGoal) valid(m *Mob, pos cube.Pos) bool {
	if _, air := m.tx.Block(pos.Side(cube.FaceUp)).(block.Air); !air {
		return false
	}
	b := m.tx.Block(pos)
	if !blockInTag(b, "minecraft:cats_can_sit_on") {
		return false
	}
	switch b := b.(type) {
	case block.Chest:
		return !b.Opened()
	case block.Furnace:
		return b.Lit
	case block.Bed:
		return !b.Head
	}
	return true
}

func (g *catSitOnBlockGoal) CanUse(m *Mob) bool {
	return g.cat.tamed && !g.cat.orderedToSit && g.moveToBlock.canUse(m, 8, 1, g.valid)
}

func (g *catSitOnBlockGoal) CanContinueToUse(m *Mob) bool {
	return g.moveToBlock.canContinueToUse(m, g.valid)
}

func (g *catSitOnBlockGoal) RequiresUpdateEveryTick() bool { return true }

func (g *catSitOnBlockGoal) Start(m *Mob) {
	g.moveToBlock.start(m, .8)
	g.cat.inSittingPose = false
}

func (g *catSitOnBlockGoal) Stop(*Mob) { g.cat.inSittingPose = false }

func (g *catSitOnBlockGoal) Tick(m *Mob) {
	g.moveToBlock.tick(m, .8)
	g.cat.inSittingPose = g.reached
}

// catRelaxOnOwnerGoal ports Cat.CatRelaxOnOwnerGoal: a tamed cat lies down
// at the foot of the bed its owner sleeps in, and may bring a gift from the
// cat_morning_gift loot table when its owner wakes up in the morning.
type catRelaxOnOwnerGoal struct {
	goalFlags
	cat        *CatBehaviour
	owner      *world.EntityHandle
	goalPos    cube.Pos
	hasGoal    bool
	onBedTicks int
}

// sleeping reports if the owner sleeps.
func (g *catRelaxOnOwnerGoal) sleeping(owner world.Entity) bool {
	s, ok := owner.(interface{ Sleeping() (cube.Pos, bool) })
	if !ok {
		return false
	}
	_, sleeping := s.Sleeping()
	return sleeping
}

func (g *catRelaxOnOwnerGoal) CanUse(m *Mob) bool {
	c := g.cat
	if !c.tamed || c.orderedToSit {
		return false
	}
	owner := c.ownerEntity(m.tx)
	if owner == nil || !isPlayerEntity(owner) {
		return false
	}
	g.owner = owner.H()
	if !g.sleeping(owner) || m.DistanceToSqr(owner) > 100 {
		return false
	}
	ownerPos := cube.PosFromVec3(owner.Position())
	b := m.tx.Block(ownerPos)
	if !blockInTag(b, "minecraft:cats_can_lie_on") {
		return false
	}
	g.goalPos, g.hasGoal = ownerPos, true
	if bed, ok := b.(block.Bed); ok {
		g.goalPos = ownerPos.Side(bed.Facing.Opposite().Face())
	}
	return !g.spaceOccupied(m)
}

// spaceOccupied reports if another cat lies near the foot of the bed.
func (g *catRelaxOnOwnerGoal) spaceOccupied(m *Mob) bool {
	box := cube.Box(0, 0, 0, 1, 1, 1).Translate(g.goalPos.Vec3()).Grow(2)
	for e := range m.tx.EntitiesWithin(box) {
		if c, ok := e.(*Cat); ok && e.H() != m.H() && (c.b().lying || c.b().relaxStateOne) {
			return true
		}
	}
	return false
}

func (g *catRelaxOnOwnerGoal) CanContinueToUse(m *Mob) bool {
	c := g.cat
	if !c.tamed || c.orderedToSit || g.owner == nil || !g.hasGoal {
		return false
	}
	owner, ok := g.owner.Entity(m.tx)
	return ok && g.sleeping(owner) && !g.spaceOccupied(m)
}

func (g *catRelaxOnOwnerGoal) Start(m *Mob) {
	g.cat.inSittingPose = false
	m.AI.nav.MoveToPos(m, float64(g.goalPos[0]), float64(g.goalPos[1]), float64(g.goalPos[2]), 1.1)
}

func (g *catRelaxOnOwnerGoal) Stop(m *Mob) {
	c := g.cat
	c.lying = false
	if owner, ok := g.owner.Entity(m.tx); ok {
		if s, ok := owner.(interface{ SleepTimer() int }); ok && s.SleepTimer() >= 100 && float64(m.Random().NextFloat()) < catGiftChance(m.tx) {
			g.giveMorningGift(m)
		}
	}
	g.onBedTicks = 0
	c.relaxStateOne = false
	m.AI.nav.Stop()
}

// catGiftChance ports the cat_waking_up_gift_chance environment attribute of
// the overworld: seven in ten from shortly before to shortly after sunrise.
func catGiftChance(tx *world.Tx) float64 {
	if tx.World().Dimension() != world.Overworld {
		return 0
	}
	if t := tx.World().Time() % 24000; t >= 23667 || t < 362 {
		return .7
	}
	return 0
}

// giveMorningGift ports CatRelaxOnOwnerGoal.giveMorningGift: the cat
// teleports near where it lies and drops its gift in front of it.
func (g *catRelaxOnOwnerGoal) giveMorningGift(m *Mob) {
	r := m.Random()
	pos := m.BlockPos()
	x := float64(pos[0] + int(r.NextInt(11)) - 5)
	y := float64(pos[1] + int(r.NextInt(5)) - 2)
	z := float64(pos[2] + int(r.NextInt(11)) - 5)
	randomTeleport(m, x, y, z, func(b world.Block) bool { return blockInTag(b, "minecraft:cat_does_not_teleport_to") }, func(float64, float64, float64) bool { return true })
	pos = m.BlockPos()
	stacks, err := loot.Generate("minecraft:gameplay/cat_morning_gift", int64(r.NextLong()), loot.Context{Tx: m.tx, Origin: m.data.Pos, ThisEntity: m.Self})
	if err != nil {
		return
	}
	sin, cos := yawSinCos(m.AI.bodyYaw)
	for _, s := range stacks {
		m.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{float64(pos[0]) - sin, float64(pos[1]), float64(pos[2]) + cos}}, s))
	}
}

func (g *catRelaxOnOwnerGoal) Tick(m *Mob) {
	owner, ok := g.owner.Entity(m.tx)
	if !ok || !g.hasGoal {
		return
	}
	c := g.cat
	c.inSittingPose = false
	m.AI.nav.MoveToPos(m, float64(g.goalPos[0]), float64(g.goalPos[1]), float64(g.goalPos[2]), 1.1)
	if m.DistanceToSqr(owner) >= 2.5 {
		c.lying = false
		return
	}
	g.onBedTicks++
	if g.onBedTicks > adjustedTickDelay(g, 16) {
		c.lying, c.relaxStateOne = true, false
		return
	}
	m.lookAt(owner, 45, 45)
	c.relaxStateOne = true
}

// lookAt ports Mob.lookAt: the mob turns its body and head towards an
// entity by at most the degrees passed.
func (m *Mob) lookAt(e world.Entity, maxYaw, maxPitch float64) {
	d := e.Position().Sub(m.data.Pos)
	dy := entityEyeY(e) - m.EyeY()
	yaw := math.Atan2(d[2], d[0])*180/math.Pi - 90
	pitch := -math.Atan2(dy, math.Hypot(d[0], d[2])) * 180 / math.Pi
	m.data.Rot = cube.Rotation{rotlerp(m.Yaw(), yaw, maxYaw), rotlerp(m.data.Rot.Pitch(), pitch, maxPitch)}
}
