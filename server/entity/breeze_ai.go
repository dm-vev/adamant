package entity

import (
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports BreezeAi and the behaviours of the breeze. The client of
// a viewer plays the sounds of a breeze inhaling, charging and sliding with
// its animations, so only the others are sent.

// newBreezeBrain ports BreezeAi and the brain provider of Breeze.
func newBreezeBrain(s *breezeState) *Brain {
	br := newBrain(breezeAttackEntitySensor{}, hurtBySensor{}, playerSensor{})
	br.addActivity(activityCore, 0, newSwim(.8), newLookAtTargetSink(45, 90))
	br.addActivityWith(activityIdle, []prioritizedBehavior{
		{0, newStartAttacking(nil, func(m *Mob) world.Entity { return m.Brain().entity(m, memNearestAttackable) })},
		{1, newStartAttacking(nil, func(m *Mob) world.Entity { return m.Brain().entity(m, memHurtByEntity) })},
		{2, newBreezeSlideToTargetSink(s, 20, 40)},
		{3, newRunOne(nil,
			weightedBehavior{newDoNothing(20, 100), 1},
			weightedBehavior{newRandomStroll(float64(float32(.6)), true), 2},
		)},
	}, nil, nil)
	attackable := breezeAttackableLastTicks(100)
	br.addActivityWith(activityFight, []prioritizedBehavior{
		{0, newStopAttackingIfTargetInvalid(func(m *Mob, t world.Entity) bool { return !attackable(m, t) }, nil, false)},
		{1, newBreezeShoot(s)},
		{2, newBreezeLongJump(s)},
		{3, newBreezeShootWhenStuck()},
		{4, newBreezeSlide(s)},
	}, []memoryCondition{{memAttackTarget, memPresent}, {memWalkTarget, memAbsent}}, nil)
	// Breeze.makeBrain.
	br.defaultAct = activityFight
	br.useDefaultActivity()
	return br
}

// breezeAttackableLastTicks ports Sensor.wasEntityAttackableLastNTicks: a
// target stays attackable for ticks checks after it last was.
func breezeAttackableLastTicks(ticks int) func(m *Mob, t world.Entity) bool {
	left := 0
	return func(m *Mob, t world.Entity) bool {
		if sensorAttackable(m, t) {
			left = ticks
			return true
		}
		left--
		return left >= 0
	}
}

// breezeAttackEntitySensor ports BreezeAttackEntitySensor: the nearest living
// entity the breeze may attack, players in creative mode left out.
type breezeAttackEntitySensor struct{ nearestLivingEntitySensor }

func (s breezeAttackEntitySensor) requires() []memoryType {
	return append(s.nearestLivingEntitySensor.requires(), memNearestAttackable)
}

func (s breezeAttackEntitySensor) doTick(m *Mob, b *Brain) {
	s.nearestLivingEntitySensor.doTick(m, b)
	for _, e := range nearestLiving(m) {
		if g, ok := e.(interface{ GameMode() world.GameMode }); ok && g.GameMode() == world.GameModeCreative {
			continue
		}
		if sensorAttackable(m, e) {
			b.setEntity(memNearestAttackable, e)
			return
		}
	}
	b.erase(memNearestAttackable)
}

// newBreezeSlideToTargetSink ports BreezeAi.SlideToTargetSink: a breeze
// slides where it walks, and shoots soon after it slid up to its target.
func newBreezeSlideToTargetSink(s *breezeState, minTimeout, maxTimeout int) *behavior {
	bh := newMoveToTargetSink(minTimeout, maxTimeout, nil)
	start, stop := bh.start, bh.stop
	bh.start = func(m *Mob, time int64) {
		start(m, time)
		s.setPose(m, breezeSliding)
	}
	bh.stop = func(m *Mob, time int64) {
		stop(m, time)
		s.setPose(m, breezeStanding)
		if m.Brain().has(memAttackTarget) {
			m.Brain().setWithExpiry(memBreezeShoot, true, 60)
		}
	}
	return bh
}

// newBreezeShoot ports Shoot: a standing breeze within sixteen blocks of its
// target charges for three quarters of a second and shoots a wind charge.
func newBreezeShoot(s *breezeState) *behavior {
	const chargeTicks, recoverTicks, cooldownTicks = 15, 4, 10
	bh := &behavior{
		entry: []memoryCondition{
			{memAttackTarget, memPresent}, {memBreezeShootCooldown, memAbsent}, {memBreezeShootCharging, memAbsent},
			{memBreezeShootRecovering, memAbsent}, {memBreezeShoot, memPresent}, {memWalkTarget, memAbsent}, {memBreezeJumpTarget, memAbsent},
		},
		minDuration: chargeTicks + 1 + recoverTicks, maxDuration: chargeTicks + 1 + recoverTicks,
	}
	bh.checkExtra = func(m *Mob) bool {
		if s.pose != breezeStanding {
			return false
		}
		b := m.Brain()
		target := b.entity(m, memAttackTarget)
		if target == nil {
			return false
		}
		if m.data.Pos.Sub(target.Position()).LenSqr() >= 256 {
			b.erase(memBreezeShoot)
			return false
		}
		return true
	}
	bh.canStillUse = func(m *Mob, _ int64) bool {
		b := m.Brain()
		return b.has(memAttackTarget) && b.has(memBreezeShoot)
	}
	bh.start = func(m *Mob, _ int64) {
		b := m.Brain()
		if b.entity(m, memAttackTarget) != nil {
			s.setPose(m, breezeShooting)
		}
		b.setWithExpiry(memBreezeShootCharging, true, chargeTicks)
	}
	bh.stop = func(m *Mob, _ int64) {
		if s.pose == breezeShooting {
			s.setPose(m, breezeStanding)
		}
		b := m.Brain()
		b.setWithExpiry(memBreezeShootCooldown, true, cooldownTicks)
		b.erase(memBreezeShoot)
	}
	bh.tick = func(m *Mob, _ int64) {
		b := m.Brain()
		target := b.entity(m, memAttackTarget)
		if target == nil {
			return
		}
		breezeLookAt(m, target.Position())
		if b.has(memBreezeShootCharging) || b.has(memBreezeShootRecovering) {
			return
		}
		b.setWithExpiry(memBreezeShootRecovering, true, recoverTicks)
		breezeShootWindCharge(m, target)
	}
	return bh
}

// breezeShootWindCharge shoots a wind charge from the middle of the breeze
// at its target, as Shoot.tick does with Projectile.spawnProjectileUsingShoot.
func breezeShootWindCharge(m *Mob, target world.Entity) {
	firingY := m.data.Pos[1] + float64(float32(m.Height())/2) + float64(float32(.3))
	fraction := .3
	if r, ok := target.(Rider); ok && r.Riding() != nil {
		fraction = .8
	}
	tp := target.Position()
	d := mgl64.Vec3{tp[0] - m.data.Pos[0], tp[1] + target.H().Type().BBox(target).Height()*fraction - firingY, tp[2] - m.data.Pos[2]}
	difficulty, _ := world.DifficultyID(m.tx.World().Difficulty())
	origin := mgl64.Vec3{m.data.Pos[0], firingY, m.data.Pos[2]}
	m.tx.AddEntity(NewBreezeWindCharge(world.EntitySpawnOpts{Position: origin, Velocity: shootVelocity(d, float64(float32(.7)), float64(5-difficulty*4))}, m.Self))
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.breeze.shoot", Volume: 1.5, Pitch: 1})
}

// breezeLookAt ports Entity.lookAt with EntityAnchorArgument.Anchor.EYES: the
// breeze turns its head and body to a position at once.
func breezeLookAt(m *Mob, pos mgl64.Vec3) {
	dx, dy, dz := pos[0]-m.data.Pos[0], pos[1]-m.EyeY(), pos[2]-m.data.Pos[2]
	sd := math.Sqrt(dx*dx + dz*dz)
	m.SetPitch(wrapDegrees(-(math.Atan2(dy, sd) * 180 / math.Pi)))
	m.SetYaw(wrapDegrees(math.Atan2(dz, dx)*180/math.Pi - 90))
	m.AI.headYaw = m.Yaw()
}

// breezeJumpAngles are LongJump.ALLOWED_ANGLES.
var breezeJumpAngles = [...]int{40, 55, 60, 75, 80}

// newBreezeLongJump ports LongJump: a breeze inhales for half a second and
// jumps to a spot behind its target.
func newBreezeLongJump(s *breezeState) *behavior {
	const inhalingTicks = 10
	bh := &behavior{
		entry: []memoryCondition{
			{memAttackTarget, memPresent}, {memBreezeJumpCooldown, memAbsent}, {memBreezeJumpInhaling, memRegistered},
			{memBreezeJumpTarget, memRegistered}, {memBreezeShoot, memAbsent}, {memWalkTarget, memAbsent}, {memBreezeLeavingWater, memRegistered},
		},
		minDuration: 200, maxDuration: 200,
	}
	bh.checkExtra = breezeCanLongJump
	bh.canStillUse = func(m *Mob, _ int64) bool {
		return s.pose != breezeStanding && !m.Brain().has(memBreezeJumpCooldown)
	}
	bh.start = func(m *Mob, _ int64) {
		b := m.Brain()
		if !b.has(memBreezeJumpInhaling) {
			b.setWithExpiry(memBreezeJumpInhaling, true, inhalingTicks)
		}
		s.setPose(m, breezeInhaling)
		if v, ok := b.get(memBreezeJumpTarget); ok {
			breezeLookAt(m, v.(cube.Pos).Vec3Centre())
		}
	}
	bh.tick = func(m *Mob, _ int64) {
		b := m.Brain()
		inWater := m.InWater()
		if !inWater && b.has(memBreezeLeavingWater) {
			b.erase(memBreezeLeavingWater)
		}
		switch {
		case !b.has(memBreezeJumpInhaling) && s.pose == breezeInhaling:
			var velocity mgl64.Vec3
			ok := false
			if v, has := b.get(memBreezeJumpTarget); has {
				velocity, ok = breezeJumpVector(m, v.(cube.Pos).Vec3Middle())
			}
			if !ok {
				s.setPose(m, breezeStanding)
				return
			}
			if inWater {
				b.set(memBreezeLeavingWater, true)
			}
			m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.breeze.jump", Volume: 1, Pitch: 1})
			s.setPose(m, breezeLongJumping)
			m.SetYaw(m.AI.bodyYaw)
			m.AI.DiscardFriction = true
			m.data.Vel = velocity
		case s.pose == breezeLongJumping && (m.OnGround() || m.InWater() && !b.has(memBreezeLeavingWater)):
			m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.breeze.land", Volume: 1, Pitch: 1})
			s.setPose(m, breezeStanding)
			m.AI.DiscardFriction = false
			cooldown := int64(10)
			if b.has(memHurtBy) {
				cooldown = 2
			}
			b.setWithExpiry(memBreezeJumpCooldown, true, cooldown)
			b.setWithExpiry(memBreezeShoot, true, 100)
		}
	}
	bh.stop = func(m *Mob, _ int64) {
		if s.pose == breezeLongJumping || s.pose == breezeInhaling {
			s.setPose(m, breezeStanding)
		}
		b := m.Brain()
		b.erase(memBreezeJumpTarget)
		b.erase(memBreezeJumpInhaling)
		b.erase(memBreezeLeavingWater)
	}
	return bh
}

// breezeCanLongJump ports LongJump.canRun: a breeze on the ground or in
// shallow water with room above it jumps to a safe spot on the ground behind
// a target between four blocks and its follow range away that it sees.
func breezeCanLongJump(m *Mob) bool {
	if !m.OnGround() && !m.InWater() {
		return false
	}
	if m.AI.waterHeight > m.FluidJumpThreshold() || m.InLava() {
		return false
	}
	b := m.Brain()
	if b.has(memBreezeJumpTarget) {
		return true
	}
	target := b.entity(m, memAttackTarget)
	if target == nil {
		return false
	}
	d := target.Position().Sub(m.data.Pos).Len()
	if d >= m.AI.FollowRange {
		b.erase(memAttackTarget)
		return false
	}
	if d-4 <= 0 || !breezeCanJumpFrom(m) {
		return false
	}
	pos, ok := breezeSnapToSurface(m.tx, breezePointBehind(m, target))
	if !ok || breezeDangerous(m.tx.Block(pos.Side(cube.FaceDown))) {
		return false
	}
	if !breezeHasLineOfSight(m, pos.Vec3Centre()) && !breezeHasLineOfSight(m, pos.Add(cube.Pos{0, 4, 0}).Vec3Centre()) {
		return false
	}
	b.set(memBreezeJumpTarget, pos)
	return true
}

// breezeCanJumpFrom ports LongJump.canJumpFromCurrentPosition: not from honey,
// and with four blocks of air or water above.
func breezeCanJumpFrom(m *Mob) bool {
	pos := m.BlockPos()
	if _, honey := m.tx.Block(pos).(block.HoneyBlock); honey {
		return false
	}
	for i := 1; i <= 4; i++ {
		above := pos.Add(cube.Pos{0, i, 0})
		if _, air := m.tx.Block(above).(block.Air); air {
			continue
		}
		if l, ok := m.tx.Liquid(above); ok {
			if _, water := l.(block.Water); water {
				continue
			}
		}
		return false
	}
	return true
}

// breezeDangerous ports EntityType.isBlockDangerous for a breeze, which is
// not immune to fire: burning blocks and those that hurt or trap mobs.
func breezeDangerous(b world.Block) bool {
	state, err := world.CanonicalStateOf(b)
	if err != nil {
		return false
	}
	switch strings.TrimPrefix(state.Name, "minecraft:") {
	case "wither_rose", "sweet_berry_bush", "cactus", "powder_snow", "lava", "magma_block", "lava_cauldron":
		return true
	}
	return blockInTag(b, "fire") || blockInTag(b, "campfires") && state.Properties["lit"] == true
}

// breezePointBehind ports BreezeUtil.randomPointBehindTarget: a point four to
// eight blocks behind where the target looks, spread around it.
func breezePointBehind(m *Mob, target world.Entity) mgl64.Vec3 {
	r := m.Random()
	angle := float32(entityHeadYaw(target)) + 180 + float32(r.NextGaussian())*90/2
	dist := lerp32(r.NextFloat(), 4, 8)
	rad := float64(angle) * math.Pi / 180
	dir := mgl64.Vec3{-math.Sin(rad), 0, math.Cos(rad)}.Mul(float64(dist))
	return target.Position().Add(dir)
}

func lerp32(t, a, b float32) float32 { return a + t*(b-a) }

// entityHeadYaw returns the yaw of the head of an entity.
func entityHeadYaw(e world.Entity) float64 {
	if h, ok := e.(interface{ HeadYaw() float64 }); ok {
		return h.HeadYaw()
	}
	return e.Rotation().Yaw()
}

// breezeSnapToSurface ports LongJump.snapToSurface: the block above the
// ground within ten blocks below the point, or else above it.
func breezeSnapToSurface(tx *world.Tx, p mgl64.Vec3) (cube.Pos, bool) {
	for _, dy := range [...]float64{-10, 10} {
		if hit, ok := clipCollider(tx, p, p.Add(mgl64.Vec3{0, dy})); ok {
			return cube.PosFromVec3(hit).Side(cube.FaceUp), true
		}
	}
	return cube.Pos{}, false
}

// clipCollider returns where a clip of collision boxes from one point to
// another first hits a block.
func clipCollider(tx *world.Tx, from, to mgl64.Vec3) (mgl64.Vec3, bool) {
	var at mgl64.Vec3
	hit := false
	trace.TraverseBlocks(from, to, func(pos cube.Pos) bool {
		if r, ok := trace.BlockIntercept(pos, tx, pathBlockAt(tx, pos), from, to, trace.CollisionShape); ok {
			at, hit = r.Position(), true
			return false
		}
		return true
	})
	return at, hit
}

// breezeHasLineOfSight ports BreezeUtil.hasLineOfSight: no block between the
// feet of the breeze and a point within fifty blocks.
func breezeHasLineOfSight(m *Mob, target mgl64.Vec3) bool {
	if target.Sub(m.data.Pos).Len() > math.Max(50, m.AI.FollowRange) {
		return false
	}
	return clipMiss(m.tx, m.data.Pos, target)
}

// breezeJumpVector ports LongJump.calculateOptimalJumpVector: the jump at the
// first of the allowed angles, shuffled, that reaches the target.
func breezeJumpVector(m *Mob, target mgl64.Vec3) (mgl64.Vec3, bool) {
	angles := breezeJumpAngles
	r := m.Random()
	for i := len(angles); i > 1; i-- {
		j := int(r.NextInt(int32(i)))
		angles[i-1], angles[j] = angles[j], angles[i-1]
	}
	for _, angle := range angles {
		maxVelocity := float64(float32(.058333334) * float32(m.AI.FollowRange))
		v, ok := jumpVectorForAngle(m, target, maxVelocity, angle, false)
		if !ok {
			continue
		}
		if _, boosted := mobEffect(m, effect.JumpBoost); boosted {
			v = v.Add(mgl64.Vec3{0, v.Normalize()[1] * jumpBoostPower(m)})
		}
		return v, true
	}
	return mgl64.Vec3{}, false
}

// newBreezeShootWhenStuck ports ShootWhenStuck: a breeze riding, in water or
// levitating shoots soon.
func newBreezeShootWhenStuck() *behavior {
	return &behavior{
		entry: []memoryCondition{
			{memAttackTarget, memPresent}, {memBreezeJumpInhaling, memAbsent}, {memBreezeJumpTarget, memAbsent},
			{memWalkTarget, memAbsent}, {memBreezeShoot, memAbsent},
		},
		checkExtra: func(m *Mob) bool {
			_, levitating := mobEffect(m, effect.Levitation)
			return m.data.Riding != nil || m.InWater() || levitating
		},
		canStillUse: func(*Mob, int64) bool { return false },
		start:       func(m *Mob, _ int64) { m.Brain().setWithExpiry(memBreezeShoot, true, 60) },
	}
}

// newBreezeSlide ports Slide: a standing breeze on the ground slides away
// from a target close to it, or behind it or into the ring four to eight
// blocks around it.
func newBreezeSlide(s *breezeState) *behavior {
	return &behavior{
		entry: []memoryCondition{
			{memAttackTarget, memPresent}, {memWalkTarget, memAbsent}, {memBreezeJumpCooldown, memAbsent}, {memBreezeShoot, memAbsent},
		},
		checkExtra: func(m *Mob) bool {
			return m.OnGround() && !m.InWater() && s.pose == breezeStanding
		},
		canStillUse: func(*Mob, int64) bool { return false },
		start: func(m *Mob, _ int64) {
			enemy := m.Brain().entity(m, memAttackTarget)
			if enemy == nil {
				return
			}
			ep := enemy.Position()
			var pos mgl64.Vec3
			found := false
			// Breeze.withinInnerCircleRange.
			centre := m.BlockPos().Vec3Centre()
			if dx, dy, dz := ep[0]-centre[0], ep[1]-centre[1], ep[2]-centre[2]; dx*dx+dz*dz < 16 && math.Abs(dy) < 10 {
				if away, ok := DefaultRandomPosAway(m, 5, 5, ep); ok && breezeHasLineOfSight(m, away) && ep.Sub(away).LenSqr() > ep.Sub(m.data.Pos).LenSqr() {
					pos, found = away, true
				}
			}
			if !found {
				if m.Random().NextBoolean() {
					pos = breezePointBehind(m, enemy)
				} else {
					// Slide.randomPointInMiddleCircle.
					d := ep.Sub(m.data.Pos)
					dist := d.Len() - (8 + m.Random().NextDouble()*(4-8))
					pos = m.data.Pos.Add(javaNormalize(d).Mul(dist))
				}
			}
			m.Brain().set(memWalkTarget, walkTarget{target: newBlockPosTracker(cube.PosFromVec3(pos)), speed: float64(float32(.6)), closeEnough: 1})
		},
	}
}
