package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports the goals of Java's Blaze and Ghast.

func (s *netherState) mobAI() *MobAI { return &s.ai }

// aiTickBlaze runs the MobAI of a blaze.
func (s *netherState) aiTickBlaze(e *Ent, tx *world.Tx) *Movement {
	ai := &s.ai
	ai.MovementSpeed, ai.FollowRange, ai.EyeHeight = s.speed, 48, 1.53
	// Blaze.aiStep: a blaze falls slowly.
	if ai.started && !ai.onGround && e.data.Vel[1] < 0 {
		e.data.Vel[1] *= .6
	}
	self := &Blaze{e}
	mov := ai.Tick(e, self, s.registerBlazeGoals)
	if !s.dead {
		tickWaterSensitive(ai.Mob(e, self))
	}
	return mov
}

// aiTickGhast runs the MobAI of a ghast.
func (s *netherState) aiTickGhast(e *Ent, tx *world.Tx) *Movement {
	ai := &s.ai
	ai.FollowRange, ai.EyeHeight, ai.FlyingSpeed = 100, 2.6, .06
	return ai.Tick(e, &Ghast{e}, s.registerGhastGoals)
}

// registerBlazeGoals ports Blaze.registerGoals.
func (s *netherState) registerBlazeGoals(m *Mob) {
	ai := m.AI
	ai.SetPathfindingMalus(PathWater, -1)
	ai.SetPathfindingMalus(PathLava, 8)
	ai.SetPathfindingMalus(PathFireInNeighbour, 0)
	ai.SetPathfindingMalus(PathFire, 0)
	ai.Goals.Add(4, &blazeAttackGoal{goalFlags: goalFlags{GoalMove | GoalLook}})
	stroll := NewWaterAvoidingRandomStrollGoal(1)
	stroll.Probability = 0
	ai.Goals.Add(7, stroll)
	ai.Goals.Add(8, NewLookAtPlayerGoal(8))
	ai.Goals.Add(8, NewRandomLookAroundGoal())
	hurt := NewHurtByTargetGoal()
	hurt.AlertOthers = true
	ai.Targets.Add(1, hurt)
	ai.Targets.Add(2, NewNearestAttackableTargetGoal(nil, true))
}

// registerGhastGoals ports Ghast.registerGoals.
func (s *netherState) registerGhastGoals(m *Mob) {
	ai := m.AI
	ai.moveOverride = &s.ghastMove
	ai.Goals.Add(5, &ghastRandomFloatAroundGoal{goalFlags: goalFlags{GoalMove}})
	ai.Goals.Add(7, &ghastLookGoal{goalFlags{GoalLook}})
	ai.Goals.Add(7, &ghastShootFireballGoal{})
	players := NewNearestAttackableTargetGoal(nil, true)
	players.Selector = func(m *Mob, target world.Entity) bool { return math.Abs(target.Position()[1]-m.data.Pos[1]) <= 4 }
	ai.Targets.Add(1, players)
}

// customServerAiStep ports Blaze.customServerAiStep: a blaze rises to a
// target above it, up to a height that changes every five seconds.
func (s *netherState) customServerAiStep(m *Mob) {
	if s.kind != blazeKind {
		return
	}
	if s.heightTimer--; s.heightTimer <= 0 {
		s.heightTimer = 100
		r := m.Random()
		s.heightOffset = .5 + 6.891*(r.NextDouble()-r.NextDouble())
	}
	if target := m.Target(); target != nil && entityEyeY(target) > m.EyeY()+s.heightOffset && m.CanAttack(target) {
		m.data.Vel[1] += (.3 - m.data.Vel[1]) * .3
	}
}

// doHurtTarget ports Mob.doHurtTarget for a blaze.
func (s *netherState) doHurtTarget(m *Mob, target world.Entity) bool {
	l, ok := target.(Living)
	if !ok {
		return false
	}
	if _, hurt := l.Hurt(s.attack, AttackDamageSource{Attacker: m.Self}); !hurt {
		return false
	}
	l.KnockBack(m.data.Pos, .4, .4)
	return true
}

// travel ports Ghast.travel: a ghast flies.
func (s *netherState) travel(m *Mob, input mgl64.Vec3) bool {
	if s.kind != ghastKind {
		return false
	}
	m.AI.travelFlying(m, input, .02, .02, .02)
	return true
}

// setBlazeCharged sets whether a blaze burns, charging its fireballs.
func (s *netherState) setBlazeCharged(m *Mob, charged bool) {
	if s.charging != charged {
		s.charging = charged
		m.updateState()
	}
}

// blazeAttackGoal ports Blaze.BlazeAttackGoal: a blaze fights in melee up
// close and otherwise charges and shoots three fireballs at a time.
type blazeAttackGoal struct {
	goalFlags
	attackStep, attackTime, lastSeen int
}

func (g *blazeAttackGoal) CanUse(m *Mob) bool {
	target := m.Target()
	return target != nil && entityAlive(target) && m.CanAttack(target)
}

func (g *blazeAttackGoal) Start(*Mob) { g.attackStep = 0 }

func (g *blazeAttackGoal) Stop(m *Mob) {
	m.Behaviour().(*netherState).setBlazeCharged(m, false)
	g.lastSeen = 0
}

func (g *blazeAttackGoal) RequiresUpdateEveryTick() bool { return true }

func (g *blazeAttackGoal) Tick(m *Mob) {
	g.attackTime--
	target := m.Target()
	if target == nil {
		return
	}
	s := m.Behaviour().(*netherState)
	sight := m.HasLineOfSight(target)
	if sight {
		g.lastSeen = 0
	} else {
		g.lastSeen++
	}
	dist := m.DistanceToSqr(target)
	tp := target.Position()
	switch {
	case dist < 4:
		if !sight {
			return
		}
		if g.attackTime <= 0 {
			g.attackTime = 20
			m.doHurtTarget(target)
		}
		m.AI.move.SetWantedPosition(tp[0], tp[1], tp[2], 1)
	case dist < m.AI.FollowRange*m.AI.FollowRange && sight:
		xd := tp[0] - m.data.Pos[0]
		yd := tp[1] + target.H().Type().BBox(target).Height()*.5 - (m.data.Pos[1] + m.Height()*.5)
		zd := tp[2] - m.data.Pos[2]
		if g.attackTime <= 0 {
			g.attackStep++
			switch {
			case g.attackStep == 1:
				g.attackTime = 60
				s.setBlazeCharged(m, true)
			case g.attackStep <= 4:
				g.attackTime = 6
			default:
				g.attackTime, g.attackStep = 100, 0
				s.setBlazeCharged(m, false)
			}
			if g.attackStep > 1 {
				spread := math.Sqrt(math.Sqrt(dist)) * .5
				r := m.Random()
				m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.blaze.shoot", Volume: 2, Pitch: float64(r.NextFloat()-r.NextFloat())*.2 + 1})
				dir := mgl64.Vec3{xd + 2.297*spread*(r.NextDouble()-r.NextDouble()), yd, zd + 2.297*spread*(r.NextDouble()-r.NextDouble())}
				if dir.Len() > 0 {
					pos := mgl64.Vec3{m.data.Pos[0], m.data.Pos[1] + m.Height()*.5 + .5, m.data.Pos[2]}
					// AbstractHurtingProjectile starts a fireball at its
					// acceleration and speeds it up from there.
					opts := world.EntitySpawnOpts{Position: pos, Velocity: dir.Normalize().Mul(.1)}
					m.tx.AddEntity(m.tx.World().EntityRegistry().Config().SmallFireball(opts, m.Self))
				}
			}
		}
		m.AI.look.SetLookAtEntityWith(target, 10, 10)
	case g.lastSeen < 5:
		m.AI.move.SetWantedPosition(tp[0], tp[1], tp[2], 1)
	}
}

// ghastMoveControl ports Ghast.GhastMoveControl: a ghast pushes itself
// towards the position its move control wants every few ticks, and waits
// once it would run into blocks on the way. A careful ghast, as the happy
// ghast is, also keeps a block away from blocks and liquids and avoids
// those that hurt it. stopped, if set, keeps the ghast in place.
type ghastMoveControl struct {
	floatDuration int
	careful       bool
	stopped       func(m *Mob) bool
}

func (c *ghastMoveControl) tick(m *Mob) {
	mc := &m.AI.move
	if c.stopped != nil && c.stopped(m) {
		mc.operation = moveWait
		m.StopInPlace()
	}
	if mc.operation != moveTo {
		return
	}
	waited := c.floatDuration
	c.floatDuration--
	if waited > 0 {
		return
	}
	c.floatDuration += int(m.Random().NextInt(5)) + 2
	travel := mgl64.Vec3(mc.wanted).Sub(m.data.Pos)
	if !ghastCanReach(m, travel, c.careful) {
		mc.operation = moveWait
		return
	}
	m.data.Vel = m.data.Vel.Add(javaNormalize(travel).Mul(m.AI.FlyingSpeed * 5 / 3))
}

// ghastCanReach ports GhastMoveControl.canReach: the box of the ghast moves
// along travel through no blocks it may not pass, checked a block at a
// time. A careful ghast also needs every block within a block of where it
// ends up to be passable, and it passes liquids only if it is in them.
func ghastCanReach(m *Mob, travel mgl64.Vec3, careful bool) bool {
	box := m.BBox()
	dest := box.Translate(travel)
	if careful {
		ok := true
		forBlocksIn(dest.Grow(1), func(pos cube.Pos) bool {
			ok = ghastTraversable(m, pos, nil, true, false, false)
			return ok
		})
		if !ok {
			return false
		}
	}
	inWater, inLava := m.InWater(), m.InLava()
	steps := math.Max(1, math.Ceil(travel.Len()))
	visited := make(map[cube.Pos]struct{})
	for i := 1.0; i <= steps; i++ {
		swept := box.Translate(travel.Mul(i / steps))
		ok := true
		forBlocksIn(swept, func(pos cube.Pos) bool {
			if _, seen := visited[pos]; seen {
				return true
			}
			visited[pos] = struct{}{}
			if box.IntersectsWith(cube.Box(0, 0, 0, 1, 1, 1).Translate(pos.Vec3())) {
				return true
			}
			ok = ghastTraversable(m, pos, &swept, careful, inWater, inLava)
			return ok
		})
		if !ok {
			return false
		}
	}
	return true
}

// ghastTraversable ports GhastMoveControl.blockTraversalPossible. With a
// swept box, only collision boxes and liquids the box moves into count.
func ghastTraversable(m *Mob, pos cube.Pos, swept *cube.BBox, careful, water, lava bool) bool {
	b := pathBlockAt(m.tx, pos)
	if _, air := b.(block.Air); air {
		return true
	}
	collides := false
	for _, bb := range b.Model().BBox(pos, m.tx) {
		if swept == nil || bb.Translate(pos.Vec3()).IntersectsWith(*swept) {
			collides = true
			break
		}
	}
	if !careful {
		return !collides
	}
	if blockInTag(b, "happy_ghast_avoids") {
		return false
	}
	if l, ok := m.tx.Liquid(pos); ok && (swept == nil || cube.Box(0, 0, 0, 1, liquidHeight(m.tx, pos, l), 1).Translate(pos.Vec3()).IntersectsWith(*swept)) {
		switch l.(type) {
		case block.Water:
			return water
		case block.Lava:
			return lava
		}
	}
	return !collides
}

// ghastRandomFloatAroundGoal ports Ghast.RandomFloatAroundGoal: a ghast
// picks a new place to fly to when it got where it wanted or that is too
// far. distanceToBlocks, if positive, makes it prefer places in the air
// within that many blocks of a block.
type ghastRandomFloatAroundGoal struct {
	goalFlags
	distanceToBlocks int
}

func (g *ghastRandomFloatAroundGoal) CanUse(m *Mob) bool {
	mc := &m.AI.move
	if !mc.HasWanted() {
		return true
	}
	dd := mgl64.Vec3(mc.wanted).Sub(m.data.Pos).LenSqr()
	return dd < 1 || dd > 3600
}

func (g *ghastRandomFloatAroundGoal) CanContinueToUse(*Mob) bool { return false }

func (g *ghastRandomFloatAroundGoal) Start(m *Mob) {
	pos := ghastFlyToPosition(m, g.distanceToBlocks)
	m.AI.move.SetWantedPosition(pos[0], pos[1], pos[2], 1)
}

// ghastFlyToPosition ports RandomFloatAroundGoal.getSuitableFlyToPosition:
// a random point within 16 blocks and the home of the ghast, below the
// ghast if the point is above the ground.
func ghastFlyToPosition(m *Mob, distanceToBlocks int) mgl64.Vec3 {
	r := m.Random()
	random := func() mgl64.Vec3 {
		return m.data.Pos.Add(mgl64.Vec3{float64(r.NextFloat()*2-1) * 16, float64(r.NextFloat()*2-1) * 16, float64(r.NextFloat()*2-1) * 16})
	}
	var result mgl64.Vec3
	found := false
	for range 64 {
		target := random()
		if !m.withinHomeVec(target) {
			found = false
			continue
		}
		result, found = target, true
		if ghastGoodTarget(m.tx, result, distanceToBlocks) {
			return result
		}
	}
	if !found {
		result = random()
	}
	pos := cube.PosFromVec3(result)
	if height := m.tx.JavaMotionBlockingHeight(pos[0], pos[2], false); height < pos[1] && height > m.tx.Range().Min() {
		result[1] = m.data.Pos[1] - math.Abs(m.data.Pos[1]-result[1])
	}
	return result
}

// ghastGoodTarget ports RandomFloatAroundGoal.isGoodTarget: a point in the
// air with a block fewer than distanceToBlocks blocks away in any of the six
// directions.
func ghastGoodTarget(tx *world.Tx, target mgl64.Vec3, distanceToBlocks int) bool {
	if distanceToBlocks <= 0 {
		return true
	}
	pos := cube.PosFromVec3(target)
	if _, air := pathBlockAt(tx, pos).(block.Air); !air {
		return false
	}
	for _, face := range cube.Faces() {
		offset := pos
		for i := 1; i < distanceToBlocks; i++ {
			offset = offset.Side(face)
			if _, air := pathBlockAt(tx, offset).(block.Air); !air {
				return true
			}
		}
	}
	return false
}

// ghastFaceMovement ports Ghast.faceMovementDirection: a ghast faces the way
// it moves, or its target within 64 blocks.
func ghastFaceMovement(m *Mob) {
	target := m.Target()
	var d mgl64.Vec3
	switch {
	case target == nil:
		d = m.data.Vel
	case target.Position().Sub(m.data.Pos).LenSqr() < 4096:
		d = target.Position().Sub(m.data.Pos)
	default:
		return
	}
	yaw := -math.Atan2(d[0], d[2]) * 180 / math.Pi
	m.SetYaw(yaw)
	m.AI.bodyYaw = yaw
}

// javaNormalize ports Vec3.normalize: a vector of length one, or none if it
// is too short.
func javaNormalize(v mgl64.Vec3) mgl64.Vec3 {
	l := v.Len()
	if l < float64(float32(1e-5)) {
		return mgl64.Vec3{}
	}
	return v.Mul(1 / l)
}

// ghastLookGoal ports Ghast.GhastLookGoal: a ghast faces the way it moves,
// or its target within 64 blocks.
type ghastLookGoal struct{ goalFlags }

func (g *ghastLookGoal) CanUse(*Mob) bool { return true }

func (g *ghastLookGoal) RequiresUpdateEveryTick() bool { return true }

func (g *ghastLookGoal) Tick(m *Mob) { ghastFaceMovement(m) }

// ghastShootFireballGoal ports Ghast.GhastShootFireballGoal: a ghast that
// sees its target within 64 blocks warns after half a second and shoots a
// fireball after a second.
type ghastShootFireballGoal struct {
	goalFlags
	chargeTime int
}

func (g *ghastShootFireballGoal) CanUse(m *Mob) bool { return m.Target() != nil }

func (g *ghastShootFireballGoal) Start(*Mob) { g.chargeTime = 0 }

func (g *ghastShootFireballGoal) Stop(m *Mob) {
	m.Behaviour().(*netherState).setGhastCharging(m, false)
}

func (g *ghastShootFireballGoal) RequiresUpdateEveryTick() bool { return true }

func (g *ghastShootFireballGoal) Tick(m *Mob) {
	target := m.Target()
	if target == nil {
		return
	}
	s := m.Behaviour().(*netherState)
	eye := mgl64.Vec3{m.data.Pos[0], m.EyeY(), m.data.Pos[2]}
	if target.Position().Sub(m.data.Pos).LenSqr() < 4096 && lineOfSight(m.tx, eye, mgl64.Vec3{target.Position()[0], entityEyeY(target), target.Position()[2]}) {
		g.chargeTime++
		if g.chargeTime == 10 {
			r := m.Random()
			m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.ghast.warn", Volume: 10, Pitch: float64(r.NextFloat()-r.NextFloat())*.2 + 1})
		}
		if g.chargeTime == 20 {
			view := m.Rotation().Vec3()
			from := mgl64.Vec3{m.data.Pos[0] + view[0]*4, m.data.Pos[1] + m.Height()*.5 + .5, m.data.Pos[2] + view[2]*4}
			tp := target.Position()
			dir := mgl64.Vec3{tp[0] - from[0], tp[1] + target.H().Type().BBox(target).Height()*.5 - from[1], tp[2] - from[2]}
			r := m.Random()
			m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.ghast.shoot", Volume: 10, Pitch: float64(r.NextFloat()-r.NextFloat())*.2 + 1})
			if dir.Len() > 0 {
				m.tx.AddEntity(NewGhastFireball(world.EntitySpawnOpts{Position: from, Velocity: dir.Normalize().Mul(.1)}, m.Self))
			}
			g.chargeTime = -40
		}
	} else if g.chargeTime > 0 {
		g.chargeTime--
	}
	s.setGhastCharging(m, g.chargeTime > 10)
}

// setGhastCharging sets whether a ghast opens its mouth to shoot.
func (s *netherState) setGhastCharging(m *Mob, charging bool) {
	if s.charging != charging {
		s.charging = charging
		m.updateState()
	}
}

// tickWaterSensitive ports the damage water deals in LivingEntity.aiStep to
// mobs sensitive to water while they stand in water or rain.
func tickWaterSensitive(m *Mob) {
	top := mgl64.Vec3{m.data.Pos[0], m.BBox().Max()[1], m.data.Pos[2]}
	if m.InWater() || m.tx.RainingAt(m.BlockPos()) || m.tx.RainingAt(cube.PosFromVec3(top)) {
		if l, ok := m.Self.(Living); ok {
			l.Hurt(1, DrowningDamageSource{})
		}
	}
}
