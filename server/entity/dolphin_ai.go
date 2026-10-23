package entity

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports the goals and movement of Java's Dolphin.

// tickDolphin runs a tick of a dolphin: its air, its MobAI and then what
// Dolphin.tick does after it, drying out and hopping about on land.
func (b *oceanWildlifeBehaviour) tickDolphin(e *Ent, tx *world.Tx) *Movement {
	self := oceanWildlifeEntity(e)
	ai := &b.ai
	ai.MovementSpeed, ai.EyeHeight = 1.2, .3
	ai.MaxHeadYaw, ai.MaxHeadPitch = 1, 1
	if b.age < 0 {
		ai.EyeHeight = .09375
		b.age++
	} else if b.age > 0 {
		b.age--
	}
	m := ai.Mob(e, self)
	// LivingEntity.baseTick: a dolphin with its eyes under water runs out of
	// air after four minutes and drowns, and breathes in fully above it.
	if ai.started && eyeInWater(m) {
		if b.air--; b.air <= -20 {
			b.air = 0
			self.(Living).Hurt(2, DrowningDamageSource{})
			if b.dead {
				return nil
			}
		}
	} else {
		b.air = 4800
	}
	mv := ai.Tick(e, self, b.registerDolphinGoals)
	if b.dead {
		return mv
	}
	pos := cube.PosFromVec3(e.data.Pos)
	if _, wet := tx.Liquid(pos); wet || tx.RainingAt(pos) || tx.RainingAt(pos.Side(cube.FaceUp)) {
		b.moistness = 2400
		return mv
	}
	if b.moistness--; b.moistness <= 0 {
		self.(Living).Hurt(1, DryOutDamageSource{})
	}
	if ai.onGround {
		r := m.Random()
		e.data.Vel = e.data.Vel.Add(mgl64.Vec3{float64((r.NextFloat()*2 - 1) * .2), .5, float64((r.NextFloat()*2 - 1) * .2)})
		e.data.Rot[0] = float64(r.NextFloat() * 360)
		ai.onGround = false
	}
	return mv
}

// registerDolphinGoals ports Dolphin.registerGoals and the navigation and
// controls of its constructor.
func (b *oceanWildlifeBehaviour) registerDolphinGoals(m *Mob) {
	ai := m.AI
	ai.nav.SetWaterBound(false)
	ai.SetPathfindingMalus(PathWater, 0)
	ai.moveOverride = smoothSwimmingMoveControl{maxTurnX: 85, maxTurnY: 10, inWater: float64(float32(.02)), outsideWater: float64(float32(.1)), gravity: true}
	ai.lookOverride = smoothSwimmingLookControl{maxYawFromCentre: 10}
	ai.Goals.Add(0, &breathAirGoal{goalFlags: goalFlags{GoalMove | GoalLook}, air: func() int { return b.air }})
	ai.Goals.Add(0, &tryFindWaterGoal{})
	ai.Goals.Add(1, &dolphinSwimToTreasureGoal{goalFlags: goalFlags{GoalMove | GoalLook}, dolphin: b})
	ai.Goals.Add(2, &dolphinSwimWithPlayerGoal{goalFlags: goalFlags{GoalMove | GoalLook}, speed: 4})
	swim := NewRandomStrollGoal(1)
	swim.Interval, swim.Position = 10, randomSwimmablePos
	ai.Goals.Add(4, swim)
	ai.Goals.Add(4, NewRandomLookAroundGoal())
	ai.Goals.Add(5, NewLookAtPlayerGoal(6))
	ai.Goals.Add(5, &dolphinJumpGoal{goalFlags: goalFlags{GoalMove | GoalJump}, interval: reducedTickDelay(10)})
	ai.Goals.Add(6, NewMeleeAttackGoal(1.2, true))
	ai.Goals.Add(7, &dolphinMoveToItemGoal{goalFlags: goalFlags{GoalMove}, dolphin: b})
	ai.Goals.Add(8, &dolphinPlayWithItemsGoal{dolphin: b})
	ai.Goals.Add(8, &followPlayerRiddenEntityGoal{isType: func(e world.Entity) bool { _, ok := e.(*Boat); return ok }})
	ai.Goals.Add(8, &followPlayerRiddenEntityGoal{isType: func(e world.Entity) bool {
		switch e.H().Type().EncodeEntity() {
		case "minecraft:nautilus", "minecraft:zombie_nautilus":
			return true
		}
		return false
	}})
	ai.Goals.Add(9, NewAvoidEntityGoal(entityIs("minecraft:guardian", "minecraft:elder_guardian"), 8, 1, 1))
	hurt := NewHurtByTargetGoal()
	hurt.IgnoreDamageFrom = entityIs("minecraft:guardian", "minecraft:elder_guardian")
	hurt.AlertOthers = true
	ai.Targets.Add(1, hurt)
}

// canAttackEntity ports Dolphin.canAttack: baby dolphins do not attack.
func (b *oceanWildlifeBehaviour) canAttackEntity(*Mob, world.Entity) bool {
	return b.id != "minecraft:dolphin" || b.age >= 0
}

// doHurtTarget ports Mob.doHurtTarget for dolphins, which ram for 3 damage.
func (b *oceanWildlifeBehaviour) doHurtTarget(m *Mob, target world.Entity) bool {
	l, ok := target.(Living)
	if !ok {
		return false
	}
	if _, hurt := l.Hurt(3, AttackDamageSource{Attacker: m.Self}); !hurt {
		return false
	}
	l.KnockBack(m.data.Pos, .4, .4)
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.dolphin.attack", Volume: 1, Pitch: 1})
	return true
}

// travelDolphin ports Dolphin.travelInWater: a dolphin swims at the speed
// its move control sets and sinks slowly unless it chases something.
func (b *oceanWildlifeBehaviour) travelDolphin(m *Mob, input mgl64.Vec3) bool {
	if !m.InWater() {
		return false
	}
	m.AI.moveRelative(m, m.AI.speed, input)
	m.AI.moveSelf(m, m.data.Vel)
	m.data.Vel = m.data.Vel.Mul(.9)
	if m.Target() == nil {
		m.data.Vel[1] -= .005
	}
	return true
}

// Collect ports Dolphin.pickUpItem: a dolphin with nothing in its mouth
// takes a whole stack.
func (d *Dolphin) Collect(stack item.Stack) (int, bool) {
	b := d.b()
	if d.Dead() || stack.Empty() || !b.held.Empty() || mobGriefingDisabled(d.tx) {
		return 0, false
	}
	b.held = stack
	b.viewHeldItem(d.Ent)
	return stack.Count(), true
}

// HeldItems returns the item the dolphin plays with.
func (d *Dolphin) HeldItems() (item.Stack, item.Stack) { return d.b().held, item.Stack{} }

// viewHeldItem shows viewers the item a dolphin holds.
func (b *oceanWildlifeBehaviour) viewHeldItem(e *Ent) {
	viewers := e.tx.Viewers(e.data.Pos)
	for _, v := range viewers {
		v.ViewEntityItems(oceanWildlifeEntity(e))
	}
	e.tx.ReleaseViewers(viewers)
}

// dropHeld ports Dolphin.ItemGoal.dropItem: a dolphin tosses the item in
// its mouth ahead of it.
func (b *oceanWildlifeBehaviour) dropHeld(m *Mob) bool {
	if b.held.Empty() {
		return false
	}
	r := m.Random()
	yaw, pitch := m.Yaw()*math.Pi/180, m.Pitch()*math.Pi/180
	dir, pow := float64(r.NextFloat())*math.Pi*2, .02*float64(r.NextFloat())
	vel := mgl64.Vec3{
		.3*-math.Sin(yaw)*math.Cos(pitch) + math.Cos(dir)*pow,
		.3 * math.Sin(pitch) * 1.5,
		.3*math.Cos(yaw)*math.Cos(pitch) + math.Sin(dir)*pow,
	}
	opts := world.EntitySpawnOpts{Position: mgl64.Vec3{m.data.Pos[0], m.EyeY() - float64(float32(.3)), m.data.Pos[2]}, Velocity: vel}
	m.tx.AddEntity(NewItemPickupDelay(opts, b.held, 2*time.Second))
	b.held = item.Stack{}
	b.viewHeldItem(m.Ent)
	return true
}

// smoothSwimmingMoveControl ports SmoothSwimmingMoveControl: a mob in water
// turns smoothly towards where it wants to go and swims there, pitching up
// or down, and slows down to turn out of water.
type smoothSwimmingMoveControl struct {
	maxTurnX, maxTurnY    float64
	inWater, outsideWater float64
	gravity               bool
}

func (c smoothSwimmingMoveControl) tick(m *Mob) {
	if c.gravity && m.InWater() {
		m.data.Vel[1] += .005
	}
	mc := &m.AI.move
	if mc.operation != moveTo || m.AI.nav.IsDone() {
		m.SetSpeed(0)
		m.SetXxa(0)
		m.SetYya(0)
		m.SetZza(0)
		return
	}
	xd, yd, zd := mc.wanted[0]-m.data.Pos[0], mc.wanted[1]-m.data.Pos[1], mc.wanted[2]-m.data.Pos[2]
	if xd*xd+yd*yd+zd*zd < 2.5000003e-7 {
		m.SetZza(0)
		return
	}
	yawD := math.Atan2(zd, xd)*180/math.Pi - 90
	yaw := moveRotlerp(m.Yaw(), yawD, c.maxTurnY)
	m.SetYaw(yaw)
	m.AI.bodyYaw, m.AI.headYaw = yaw, yaw
	speed := mc.speedModifier * m.AI.MovementSpeed
	if !m.InWater() {
		left := math.Abs(wrapDegrees(m.Yaw() - yawD))
		m.SetSpeed(speed * c.outsideWater * (1 - math.Max(0, math.Min(1, (left-10)/50))))
		return
	}
	m.SetSpeed(speed * c.inWater)
	if sd := math.Sqrt(xd*xd + zd*zd); math.Abs(yd) > 1e-5 || math.Abs(sd) > 1e-5 {
		pitchD := math.Max(-c.maxTurnX, math.Min(c.maxTurnX, wrapDegrees(-(math.Atan2(yd, sd)*180/math.Pi))))
		m.SetPitch(rotateTowards(m.Pitch(), pitchD, 5))
	}
	// The input takes the full speed, which travelInWater scales by the
	// speed of the mob in water.
	cos, sin := math.Cos(m.Pitch()*math.Pi/180), math.Sin(m.Pitch()*math.Pi/180)
	m.SetZza(cos * speed)
	m.SetYya(-sin * speed)
}

// smoothSwimmingLookControl ports SmoothSwimmingLookControl: a mob tilts its
// head a little past what it looks at, levels out once it stops swimming,
// and turns its body to keep its head within reach.
type smoothSwimmingLookControl struct{ maxYawFromCentre float64 }

func (c smoothSwimmingLookControl) tick(m *Mob) {
	l := &m.AI.look
	if l.cooldown > 0 {
		l.cooldown--
		if yaw, ok := l.yawD(m); ok {
			m.AI.headYaw = rotateTowards(m.AI.headYaw, yaw+20, l.yMaxRotSpeed)
		}
		if pitch, ok := l.pitchD(m); ok {
			m.SetPitch(rotateTowards(m.Pitch(), pitch+10, l.xMaxRotAngle))
		}
	} else {
		if m.AI.nav.IsDone() {
			m.SetPitch(rotateTowards(m.Pitch(), 0, 5))
		}
		m.AI.headYaw = rotateTowards(m.AI.headYaw, m.AI.bodyYaw, l.yMaxRotSpeed)
	}
	if diff := wrapDegrees(m.AI.headYaw - m.AI.bodyYaw); diff < -c.maxYawFromCentre {
		m.AI.bodyYaw -= 4
	} else if diff > c.maxYawFromCentre {
		m.AI.bodyYaw += 4
	}
}

// breathAirGoal ports BreathAirGoal: a mob low on air swims up to the
// nearest air above it, or eight blocks up.
type breathAirGoal struct {
	goalFlags
	air func() int
}

func (g *breathAirGoal) CanUse(*Mob) bool { return g.air() < 140 }

func (g *breathAirGoal) IsInterruptable() bool { return false }

func (g *breathAirGoal) Start(m *Mob) {
	m.AI.nav.Stop()
	g.findAirPosition(m)
}

func (g *breathAirGoal) Tick(m *Mob) {
	g.findAirPosition(m)
	m.AI.moveRelative(m, float64(float32(.02)), mgl64.Vec3{m.AI.xxa, m.AI.yya, m.AI.zza})
	m.AI.moveSelf(m, m.data.Vel)
}

func (g *breathAirGoal) findAirPosition(m *Mob) {
	pos := m.BlockPos()
	dest := cube.PosFromVec3(mgl64.Vec3{m.data.Pos[0], m.data.Pos[1] + 8, m.data.Pos[2]})
	// BlockPos.neighborColumn walks up from the block of the mob.
	for y := pos.Y(); y <= pos.Y()+8; y++ {
		p := cube.Pos{pos.X(), y, pos.Z()}
		_, wet := m.tx.Liquid(p)
		b := pathBlockAt(m.tx, p)
		_, bubbles := b.(block.BubbleColumn)
		if (!wet || bubbles) && pathfindableLand(m.tx, p, b) {
			dest = p
			break
		}
	}
	m.AI.nav.MoveToPos(m, float64(dest.X()), float64(dest.Y()+1), float64(dest.Z()), 1)
}

// tryFindWaterGoal ports TryFindLiquidGoal for water: a mob on dry ground
// heads for water within two blocks around and below it.
type tryFindWaterGoal struct{ goalFlags }

func (g *tryFindWaterGoal) CanUse(m *Mob) bool { return m.OnGround() && !pathWater(m.tx, m.BlockPos()) }

func (g *tryFindWaterGoal) Start(m *Mob) {
	x, y, z := m.data.Pos[0], m.data.Pos[1], m.data.Pos[2]
	maxY := m.BlockPos().Y()
	for zz := int(math.Floor(z - 2)); zz <= int(math.Floor(z+2)); zz++ {
		for yy := int(math.Floor(y - 2)); yy <= maxY; yy++ {
			for xx := int(math.Floor(x - 2)); xx <= int(math.Floor(x+2)); xx++ {
				if pathWater(m.tx, cube.Pos{xx, yy, zz}) {
					m.AI.move.SetWantedPosition(float64(xx), float64(yy), float64(zz), 1)
					return
				}
			}
		}
	}
}

// dolphinSwimToTreasureGoal ports DolphinSwimToTreasureGoal: a dolphin fed a
// fish leads the way to the nearest ocean ruin or shipwreck within 50
// chunks.
type dolphinSwimToTreasureGoal struct {
	goalFlags
	dolphin *oceanWildlifeBehaviour
	stuck   bool
}

func (g *dolphinSwimToTreasureGoal) IsInterruptable() bool { return false }

func (g *dolphinSwimToTreasureGoal) CanUse(*Mob) bool {
	return g.dolphin.gotFish && g.dolphin.air >= 100
}

func (g *dolphinSwimToTreasureGoal) reached(m *Mob) bool {
	b := g.dolphin
	pos := cube.PosFromVec3(mgl64.Vec3{float64(b.treasureX), m.data.Pos[1], float64(b.treasureZ)})
	return pos.Vec3Centre().Sub(m.data.Pos).LenSqr() < 16
}

func (g *dolphinSwimToTreasureGoal) CanContinueToUse(m *Mob) bool {
	b := g.dolphin
	return b.hasTreasure && !g.reached(m) && !g.stuck && b.air >= 100
}

func (g *dolphinSwimToTreasureGoal) Start(m *Mob) {
	b := g.dolphin
	g.stuck = false
	m.AI.nav.Stop()
	if target, found, _ := m.tx.World().FindNearestMapFeature("#minecraft:dolphin_located", m.BlockPos(), 50, false); found {
		b.treasureX, b.treasureY, b.treasureZ = target.Position.X(), target.Position.Y(), target.Position.Z()
		b.hasTreasure = true
	} else {
		g.stuck = true
	}
}

func (g *dolphinSwimToTreasureGoal) Stop(m *Mob) {
	b := g.dolphin
	if !b.hasTreasure || g.reached(m) || g.stuck {
		b.gotFish = false
	}
}

func (g *dolphinSwimToTreasureGoal) Tick(m *Mob) {
	b := g.dolphin
	if !b.hasTreasure {
		return
	}
	// Dolphin.closeToNextPos.
	target, ok := m.AI.nav.TargetPos()
	close := ok && target.Vec3Centre().Sub(m.data.Pos).LenSqr() < 144
	if !close && !m.AI.nav.IsDone() {
		return
	}
	treasure := cube.Pos{b.treasureX, b.treasureY, b.treasureZ}.Vec3Centre()
	next, found := DefaultRandomPosTowards(m, 16, 1, treasure, math.Pi/8)
	if !found {
		next, found = DefaultRandomPosTowards(m, 8, 4, treasure, math.Pi/2)
	}
	if found {
		if p := cube.PosFromVec3(next); !pathWater(m.tx, p) || !pathfindableWater(m, p) {
			next, found = DefaultRandomPosTowards(m, 8, 5, treasure, math.Pi/2)
		}
	}
	if !found {
		g.stuck = true
		return
	}
	m.AI.look.SetLookAtWith(next[0], next[1], next[2], m.AI.MaxHeadYaw+20, m.AI.MaxHeadPitch)
	m.AI.nav.MoveToPos(m, next[0], next[1], next[2], 1.3)
}

// dolphinSwimWithPlayerGoal ports DolphinSwimWithPlayerGoal: a dolphin swims
// alongside a swimming player within ten blocks. Bedrock has no Dolphin's
// Grace for it to give, and its clients swim at their own speed.
type dolphinSwimWithPlayerGoal struct {
	goalFlags
	speed  float64
	player *world.EntityHandle
}

func swimming(e world.Entity) bool {
	s, ok := e.(interface{ Swimming() bool })
	return ok && s.Swimming()
}

func (g *dolphinSwimWithPlayerGoal) CanUse(m *Mob) bool {
	c := forNonCombat()
	c.rangeLimit, c.ignoreSight = 10, true
	p := nearestPlayer(m, c)
	if p == nil {
		return false
	}
	g.player = p.H()
	return swimming(p) && m.Target() != p
}

func (g *dolphinSwimWithPlayerGoal) CanContinueToUse(m *Mob) bool {
	p, ok := g.player.Entity(m.tx)
	return ok && swimming(p) && m.DistanceToSqr(p) < 256
}

func (g *dolphinSwimWithPlayerGoal) Stop(m *Mob) {
	g.player = nil
	m.AI.nav.Stop()
}

func (g *dolphinSwimWithPlayerGoal) Tick(m *Mob) {
	p, ok := g.player.Entity(m.tx)
	if !ok {
		return
	}
	m.AI.look.SetLookAtEntityWith(p, m.AI.MaxHeadYaw+20, m.AI.MaxHeadPitch)
	if m.DistanceToSqr(p) < 6.25 {
		m.AI.nav.Stop()
	} else {
		m.AI.nav.MoveToEntity(m, p, g.speed)
	}
}

// dolphinJumpGoal ports DolphinJumpGoal: now and then a dolphin with clear
// water and air ahead leaps out of the water.
type dolphinJumpGoal struct {
	goalFlags
	interval int
	breached bool
}

// motionDirection ports Entity.getMotionDirection: the horizontal direction
// a mob faces.
func motionDirection(m *Mob) cube.Pos {
	switch (cube.Rotation{m.Yaw(), 0}).Direction() {
	case cube.North:
		return cube.Pos{0, 0, -1}
	case cube.South:
		return cube.Pos{0, 0, 1}
	case cube.West:
		return cube.Pos{-1, 0, 0}
	}
	return cube.Pos{1, 0, 0}
}

func (g *dolphinJumpGoal) CanUse(m *Mob) bool {
	if m.Random().NextInt(int32(g.interval)) != 0 {
		return false
	}
	step, pos := motionDirection(m), m.BlockPos()
	for _, i := range [...]int{0, 1, 4, 5, 6, 7} {
		next := pos.Add(cube.Pos{step[0] * i, 0, step[2] * i})
		if !pathWater(m.tx, next) || blockInTag(m.tx.Block(next), "blocks_dolphin_jump") {
			return false
		}
		if !isAir(m.tx.Block(next.Side(cube.FaceUp))) || !isAir(m.tx.Block(next.Add(cube.Pos{0, 2, 0}))) {
			return false
		}
	}
	return true
}

func (g *dolphinJumpGoal) CanContinueToUse(m *Mob) bool {
	yd := m.data.Vel[1]
	return (yd*yd >= float64(float32(.03)) || m.Pitch() == 0 || math.Abs(m.Pitch()) >= 10 || !m.InWater()) && !m.OnGround()
}

func (g *dolphinJumpGoal) IsInterruptable() bool { return false }

func (g *dolphinJumpGoal) Start(m *Mob) {
	step := motionDirection(m)
	m.data.Vel = m.data.Vel.Add(mgl64.Vec3{float64(step[0]) * .6, .7, float64(step[2]) * .6})
	m.AI.nav.Stop()
}

func (g *dolphinJumpGoal) Stop(m *Mob) { m.SetPitch(0) }

func (g *dolphinJumpGoal) Tick(m *Mob) {
	already := g.breached
	if !already {
		g.breached = pathWater(m.tx, m.BlockPos())
	}
	if g.breached && !already {
		m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.dolphin.jump", Volume: 1, Pitch: 1})
	}
	v := m.data.Vel
	if v[1]*v[1] < float64(float32(.03)) && m.Pitch() != 0 {
		m.SetPitch(m.Pitch() + wrapDegrees(-m.Pitch())*.2)
	} else if v.Len() > 1e-5 {
		m.SetPitch(math.Atan2(-v[1], math.Hypot(v[0], v[2])) * 180 / math.Pi)
	}
}

// dolphinItems returns the items in water within eight blocks a dolphin
// may pick up, as Dolphin.ALLOWED_ITEMS selects.
func dolphinItems(m *Mob) []world.Entity {
	var items []world.Entity
	for e := range m.tx.EntitiesWithin(m.BBox().Grow(8)) {
		i, ok := e.(*Ent)
		if !ok || i.H().Type() != ItemType {
			continue
		}
		if b, ok := i.Behaviour().(*ItemBehaviour); ok && b.pickupDelay == 0 && entityInWater(m.tx, e) {
			items = append(items, e)
		}
	}
	return items
}

// dolphinMoveToItemGoal ports Dolphin.MoveToItemGoal: a dolphin swims after
// items in the water within eight blocks.
type dolphinMoveToItemGoal struct {
	goalFlags
	dolphin  *oceanWildlifeBehaviour
	cooldown int
}

func (g *dolphinMoveToItemGoal) CanUse(m *Mob) bool {
	return g.cooldown <= m.AI.tickCount && len(dolphinItems(m)) > 0
}

func (g *dolphinMoveToItemGoal) Stop(m *Mob) {
	if g.dolphin.dropHeld(m) {
		g.cooldown = m.AI.tickCount + int(m.Random().NextInt(100))
	}
}

func (g *dolphinMoveToItemGoal) Start(m *Mob) {
	if items := dolphinItems(m); len(items) > 0 {
		m.AI.nav.MoveToEntity(m, items[0], float64(float32(1.2)))
		m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.dolphin.play", Volume: 1, Pitch: 1})
	}
	g.cooldown = 0
}

func (g *dolphinMoveToItemGoal) Tick(m *Mob) {
	if items := dolphinItems(m); len(items) > 0 {
		m.AI.nav.MoveToEntity(m, items[0], float64(float32(1.2)))
	}
}

// dolphinPlayWithItemsGoal ports Dolphin.PlayWithItemsGoal: a dolphin tosses
// away whatever it holds.
type dolphinPlayWithItemsGoal struct {
	goalFlags
	dolphin *oceanWildlifeBehaviour
}

func (g *dolphinPlayWithItemsGoal) CanUse(*Mob) bool { return !g.dolphin.held.Empty() }

func (g *dolphinPlayWithItemsGoal) Stop(m *Mob) { g.dolphin.dropHeld(m) }

func (g *dolphinPlayWithItemsGoal) Tick(m *Mob) { g.dolphin.dropHeld(m) }

// followPlayerRiddenEntityGoal ports FollowPlayerRiddenEntityGoal: a mob
// swims behind a boat a player steers within five blocks, then ahead of it.
type followPlayerRiddenEntityGoal struct {
	goalFlags
	isType           func(world.Entity) bool
	following        *world.EntityHandle
	timeToRecalcPath int
	ahead            bool
}

// steeredVehicle returns the player steering a vehicle, if any, as
// Entity.getControllingPassenger.
func steeredVehicle(tx *world.Tx, vehicle world.Entity) world.Entity {
	e, ok := vehicle.(interface{ Behaviour() Behaviour })
	if !ok {
		return nil
	}
	driver, ok := e.Behaviour().(interface{ Passenger() *world.EntityHandle })
	if !ok || driver.Passenger() == nil {
		return nil
	}
	if p, ok := driver.Passenger().Entity(tx); ok && isPlayerEntity(p) {
		return p
	}
	return nil
}

// movedRecently ports Entity.hasMovedHorizontallyRecently for a player
// riding a vehicle.
func movedRecently(tx *world.Tx, player world.Entity) bool {
	r, ok := player.(interface{ Riding() *world.EntityHandle })
	if !ok || r.Riding() == nil {
		return false
	}
	vehicle, ok := r.Riding().Entity(tx)
	if !ok {
		return false
	}
	v, ok := vehicle.(interface{ Velocity() mgl64.Vec3 })
	return ok && math.Hypot(v.Velocity()[0], v.Velocity()[2]) > 1e-5
}

func (g *followPlayerRiddenEntityGoal) CanUse(m *Mob) bool {
	if g.following != nil {
		if p, ok := g.following.Entity(m.tx); ok && movedRecently(m.tx, p) {
			return true
		}
	}
	for e := range m.tx.EntitiesWithin(m.BBox().Grow(5)) {
		if !g.isType(e) {
			continue
		}
		if p := steeredVehicle(m.tx, e); p != nil && movedRecently(m.tx, p) {
			return true
		}
	}
	return false
}

func (g *followPlayerRiddenEntityGoal) CanContinueToUse(m *Mob) bool {
	if g.following == nil {
		return false
	}
	p, ok := g.following.Entity(m.tx)
	return ok && movedRecently(m.tx, p)
}

func (g *followPlayerRiddenEntityGoal) Start(m *Mob) {
	for e := range m.tx.EntitiesWithin(m.BBox().Grow(5)) {
		if !g.isType(e) {
			continue
		}
		if p := steeredVehicle(m.tx, e); p != nil {
			g.following = p.H()
			break
		}
	}
	g.timeToRecalcPath, g.ahead = 0, false
}

func (g *followPlayerRiddenEntityGoal) Stop(*Mob) { g.following = nil }

func (g *followPlayerRiddenEntityGoal) Tick(m *Mob) {
	p, ok := g.following.Entity(m.tx)
	if !ok {
		return
	}
	speed := float64(float32(.015))
	if g.ahead {
		speed = float64(float32(.01))
	}
	m.AI.moveRelative(m, speed, mgl64.Vec3{m.AI.xxa, m.AI.yya, m.AI.zza})
	m.AI.moveSelf(m, m.data.Vel)
	if g.timeToRecalcPath--; g.timeToRecalcPath > 0 {
		return
	}
	g.timeToRecalcPath = adjustedTickDelay(g, 10)
	pos := cube.PosFromVec3(p.Position())
	facing := (cube.Rotation{p.Rotation().Yaw(), 0}).Direction()
	if !g.ahead {
		behind := pos.Side(facing.Opposite().Face()).Sub(cube.Pos{0, 1, 0})
		m.AI.nav.MoveToPos(m, float64(behind.X()), float64(behind.Y()), float64(behind.Z()), 1)
		if p.Position().Sub(m.data.Pos).Len() < 4 {
			g.timeToRecalcPath, g.ahead = 0, true
		}
		return
	}
	step := facing.Face()
	goTo := pos
	for range 10 {
		goTo = goTo.Side(step)
	}
	m.AI.nav.MoveToPos(m, float64(goTo.X()), float64(goTo.Y()-1), float64(goTo.Z()), 1)
	if p.Position().Sub(m.data.Pos).Len() > 12 {
		g.timeToRecalcPath, g.ahead = 0, false
	}
}
