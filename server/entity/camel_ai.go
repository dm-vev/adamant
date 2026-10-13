package entity

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// camelState is the state of the idle activity of CamelAi.
type camelState struct {
	// idle counts down DoNothing, walking is set while a walk target set by
	// the idle activity is followed on nav.
	idle    int
	walking bool
	nav     Navigator
	// look is the LOOK_TARGET SetEntityLookTargetSometimes sets every 30 to
	// 60 ticks, which lookTicks counts down to.
	look      *world.EntityHandle
	lookTicks int
	panicking bool
}

// camelGoals returns the goals of CamelAi's core and idle activities: a
// four times faster AnimalPanic, AnimalMakeLove, FollowTemptation and
// BabyFollowAdult at 2.5 times the speed.
func (b *MountBehaviour) camelGoals(m *Mount) animalGoalSpec {
	s := animalGoalSpec{speed: b.movementSpeed, panic: 4, breed: 1, tempt: 2.5, tempting: m.breedingFood, temptStop: 3.5, parent: 2.5,
		parentRange: parentRange{start: 5, stop: 4, far: 17, search: 16}, reach: 1, species: b}
	if b.ageTicks < 0 {
		s.temptStop = 2.5
	}
	if b.mobControlled() {
		s.panic = 0
	}
	return s
}

// tickCamel runs CamelAi and returns the velocity and rotation it gives the
// camel.
func (b *MountBehaviour) tickCamel(m *Mount, tx *world.Tx) (mgl64.Vec3, cube.Rotation) {
	e := m.Ent
	c := &b.camelState
	vel, rot := b.goals.tick(e, tx, m, b.camelGoals(m))
	if panicking := b.goals.panic.active; panicking != c.panicking {
		c.panicking = panicking
		if panicking {
			// CamelPanic stands the camel up at once.
			b.standUpInstantly(m)
		}
	}
	if _, inWater := tx.Liquid(cube.PosFromVec3(e.Position())); inWater && b.sitting {
		b.standUpInstantly(m)
	}
	b.lookSometimes(m, tx)
	if b.goals.running == goalIdle || b.goals.running == goalSpecies {
		if v, r, moving := b.camelIdle(m, tx); moving {
			vel, rot = v, r
		}
	} else {
		c.walking, c.idle = false, 0
	}
	if b.refuseToMove(tx) {
		// Camel.travel: a sitting camel or one changing its pose stays put.
		vel, rot = e.data.Vel, e.data.Rot
		vel[0], vel[2] = 0, 0
	}
	return vel, rot
}

// lookSometimes ports SetEntityLookTargetSometimes: every one and a half to
// three seconds the camel picks a visible player within six blocks to look
// at.
func (b *MountBehaviour) lookSometimes(m *Mount, tx *world.Tx) {
	c := &b.camelState
	if c.lookTicks > 0 {
		c.lookTicks--
		return
	}
	c.lookTicks = 30 + rand.IntN(31)
	c.look = nil
	if p := lookTargetPlayer(m.Ent, tx, 6); p != nil && lineVisible(m.Ent, tx, p) {
		c.look = p.H()
	}
}

// camelIdle ports the RunOne of CamelAi's idle activity: each time it has
// nothing to walk to, the camel strolls, walks up to what it looks at, sits
// down or stands up, or does nothing for a while, whichever of these in
// random order it can.
func (b *MountBehaviour) camelIdle(m *Mount, tx *world.Tx) (mgl64.Vec3, cube.Rotation, bool) {
	c := &b.camelState
	if c.walking {
		vel, rot, moving := steer(m.Ent, &c.nav, groundSpeed(b.movementSpeed, 2), 1)
		if moving {
			return vel, rot, true
		}
		c.walking = false
		return m.data.Vel, m.data.Rot, false
	}
	if c.idle > 0 {
		c.idle--
		return mgl64.Vec3{}, cube.Rotation{}, false
	}
	refuse := b.refuseToMove(tx)
	for _, option := range rand.Perm(4) {
		switch option {
		case 0:
			if refuse {
				continue
			}
			mob := newRandomPosMob(m.Ent, tx)
			if goal, ok := mob.randomPos(10, 7); ok && c.nav.MoveTo(tx, cube.PosFromVec3(m.Position()), goal, mob.profile, navigatorTick(m.Ent)) {
				c.walking = true
				return mgl64.Vec3{}, cube.Rotation{}, false
			}
		case 1:
			if refuse || c.look == nil {
				continue
			}
			look, ok := c.look.Entity(tx)
			if !ok || look.Position().Sub(m.Position()).LenSqr() < 9 {
				continue
			}
			c.nav = Navigator{}
			navigateTo(&c.nav, m.Ent, tx, look.Position())
			if len(c.nav.Path) != 0 {
				c.walking = true
				return mgl64.Vec3{}, cube.Rotation{}, false
			}
		case 2:
			if b.randomSitting(m, tx) {
				return mgl64.Vec3{}, cube.Rotation{}, false
			}
		case 3:
			c.idle = 30 + rand.IntN(31)
			return mgl64.Vec3{}, cube.Rotation{}, false
		}
	}
	return mgl64.Vec3{}, cube.Rotation{}, false
}

// randomSitting ports CamelAi.RandomSitting: a camel on the ground that kept
// its pose for 20 seconds sits down or stands up, unless it is in water, on
// a lead, steered or panicking.
func (b *MountBehaviour) randomSitting(m *Mount, tx *world.Tx) bool {
	if _, inWater := tx.Liquid(cube.PosFromVec3(m.Position())); inWater || b.poseTime(tx) < 400 || m.HolderLeashID() != 0 || !b.mc.OnGround() || b.controlled() && b.vehicle() {
		return false
	}
	if b.sitting {
		b.standUp(m)
	} else if !b.goals.panic.active {
		b.sitDown(m)
	}
	return true
}

// poseTime ports Camel.getPoseTime: the ticks since the camel last sat down
// or stood up.
func (b *MountBehaviour) poseTime(tx *world.Tx) int64 {
	last := b.lastPoseTick
	if last < 0 {
		last = -last
	}
	return int64(tx.World().Time()) - last
}

// refuseToMove ports Camel.refuseToMove: a camel does not move while it sits
// or sits down or stands up.
func (b *MountBehaviour) refuseToMove(tx *world.Tx) bool {
	return b.camel() && (b.sitting || b.inPoseTransition(tx))
}

// inPoseTransition ports Camel.isInPoseTransition: sitting down takes 40
// ticks and standing up 52.
func (b *MountBehaviour) inPoseTransition(tx *world.Tx) bool {
	transition := int64(52)
	if b.sitting {
		transition = 40
	}
	return b.poseTime(tx) < transition
}

// sitDown ports Camel.sitDown.
func (b *MountBehaviour) sitDown(m *Mount) {
	if b.sitting {
		return
	}
	b.sitting, b.lastPoseTick = true, -int64(m.tx.World().Time())
	b.camelState.walking = false
	m.tx.EmitGameEvent(world.GameEventEntityAction, m.Position(), world.GameEventContext{Source: m})
	m.updateState()
}

// standUp ports Camel.standUp: the camel takes 52 ticks to stand up.
func (b *MountBehaviour) standUp(m *Mount) {
	if !b.sitting {
		return
	}
	b.sitting, b.lastPoseTick = false, int64(m.tx.World().Time())
	m.tx.EmitGameEvent(world.GameEventEntityAction, m.Position(), world.GameEventContext{Source: m})
	m.updateState()
}

// standUpInstantly ports Camel.standUpInstantly.
func (b *MountBehaviour) standUpInstantly(m *Mount) {
	wasSitting := b.sitting
	b.sitting, b.lastPoseTick = false, max(0, int64(m.tx.World().Time())-52-1)
	m.tx.EmitGameEvent(world.GameEventEntityAction, m.Position(), world.GameEventContext{Source: m})
	if wasSitting {
		m.updateState()
	}
}
