package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// moveOperation is what a move control does with its wanted position, as
// MoveControl.Operation.
type moveOperation uint8

const (
	moveWait moveOperation = iota
	moveTo
	moveStrafe
	moveJumping
)

// moveControl walks a mob towards the position it wants to reach, ported
// from Java's MoveControl.
type moveControl struct {
	wanted                      [3]float64
	speedModifier               float64
	strafeForwards, strafeRight float64
	operation                   moveOperation
	// adjustWanted, if set, returns the speed modifier to walk to a wanted
	// position with, as subclasses of MoveControl override
	// setWantedPosition.
	adjustWanted func(speedModifier float64) float64
}

// HasWanted reports if the control walks towards a position.
func (c *moveControl) HasWanted() bool { return c.operation == moveTo }

// SpeedModifier returns the modifier of the speed the control walks with.
func (c *moveControl) SpeedModifier() float64 { return c.speedModifier }

// Wanted returns the position the control walks towards.
func (c *moveControl) Wanted() [3]float64 { return c.wanted }

// SetWantedPosition walks the mob towards a position with its speed scaled
// by speedModifier.
func (c *moveControl) SetWantedPosition(x, y, z, speedModifier float64) {
	if c.adjustWanted != nil {
		speedModifier = c.adjustWanted(speedModifier)
	}
	c.wanted = [3]float64{x, y, z}
	c.speedModifier = speedModifier
	if c.operation != moveJumping {
		c.operation = moveTo
	}
}

// Strafe walks the mob forwards and to its right relative to where it looks.
func (c *moveControl) Strafe(forwards, right float64) {
	c.operation = moveStrafe
	c.strafeForwards, c.strafeRight = forwards, right
	c.speedModifier = .25
}

// SetWait stops the control.
func (c *moveControl) SetWait() { c.operation = moveWait }

// tick ports MoveControl.tick.
func (c *moveControl) tick(m *Mob) {
	switch c.operation {
	case moveStrafe:
		speed := c.speedModifier * m.AI.MovementSpeed
		xa, za := c.strafeForwards, c.strafeRight
		dist := math.Max(1, math.Sqrt(xa*xa+za*za))
		dist = speed / dist
		xa, za = xa*dist, za*dist
		sin, cos := math.Sin(m.Yaw()*math.Pi/180), math.Cos(m.Yaw()*math.Pi/180)
		dx, dz := xa*cos-za*sin, za*cos+xa*sin
		if !c.walkable(m, dx, dz) {
			c.strafeForwards, c.strafeRight = 1, 0
		}
		m.SetSpeed(speed)
		m.SetZza(c.strafeForwards)
		m.SetXxa(c.strafeRight)
		c.operation = moveWait
	case moveTo:
		c.operation = moveWait
		pos := m.data.Pos
		xd, yd, zd := c.wanted[0]-pos[0], c.wanted[1]-pos[1], c.wanted[2]-pos[2]
		if xd*xd+yd*yd+zd*zd < 2.5000003e-7 {
			m.SetZza(0)
			return
		}
		yaw := math.Atan2(zd, xd)*180/math.Pi - 90
		m.SetYaw(moveRotlerp(m.Yaw(), yaw, 90))
		m.SetSpeed(c.speedModifier * m.AI.MovementSpeed)
		if yd > m.AI.StepHeight && xd*xd+zd*zd < math.Max(1, m.Width()) || c.blockedByShape(m) {
			m.AI.jump.Jump()
			c.operation = moveJumping
		}
	case moveJumping:
		m.SetSpeed(c.speedModifier * m.AI.MovementSpeed)
		if m.OnGround() || m.InLiquid() {
			c.operation = moveWait
		}
	default:
		m.SetZza(0)
	}
}

// blockedByShape reports if the mob stands in a block it should jump onto:
// one with a collision shape above its feet that is no door or fence.
func (c *moveControl) blockedByShape(m *Mob) bool {
	pos := m.BlockPos()
	b := m.tx.Block(pos)
	boxes := b.Model().BBox(pos, m.tx)
	if len(boxes) == 0 {
		return false
	}
	top := 0.0
	for _, bb := range boxes {
		top = math.Max(top, bb.Max()[1])
	}
	if m.data.Pos[1] >= top+float64(pos.Y()) {
		return false
	}
	return !blockInTag(b, "minecraft:doors") && !blockInTag(b, "minecraft:fences")
}

// walkable ports MoveControl.isWalkable.
func (c *moveControl) walkable(m *Mob, dx, dz float64) bool {
	pos := cube.PosFromVec3(m.data.Pos.Add([3]float64{dx, 0, dz}))
	pos[1] = m.BlockPos().Y()
	return m.AI.nav.eval.pathType(m, pos) == PathWalkable
}

// moveRotlerp ports MoveControl.rotlerp, which keeps the result in [0, 360].
func moveRotlerp(a, b, max float64) float64 {
	r := rotlerp(a, b, max)
	if r < 0 {
		r += 360
	} else if r > 360 {
		r -= 360
	}
	return r
}

// lookControl turns the head of a mob towards what it looks at, ported from
// Java's LookControl.
type lookControl struct {
	wanted                     [3]float64
	yMaxRotSpeed, xMaxRotAngle float64
	cooldown                   int
	// keepPitch stops the control from resetting the pitch every tick.
	keepPitch bool
}

// SetLookAtEntity looks at the eyes of an entity with the head turn limits
// of the mob.
func (c *lookControl) SetLookAtEntity(m *Mob, e world.Entity) {
	c.SetLookAt(m, e.Position()[0], entityEyeY(e), e.Position()[2])
}

// SetLookAtEntityWith looks at the eyes of an entity, turning the head by at
// most yMax and xMax degrees a tick.
func (c *lookControl) SetLookAtEntityWith(e world.Entity, yMax, xMax float64) {
	c.SetLookAtWith(e.Position()[0], entityEyeY(e), e.Position()[2], yMax, xMax)
}

// SetLookAt looks at a position with the head turn limits of the mob.
func (c *lookControl) SetLookAt(m *Mob, x, y, z float64) {
	c.SetLookAtWith(x, y, z, m.AI.HeadRotSpeed, m.AI.MaxHeadPitch)
}

// SetLookAtWith looks at a position, turning the head by at most yMax and
// xMax degrees a tick.
func (c *lookControl) SetLookAtWith(x, y, z, yMax, xMax float64) {
	c.wanted = [3]float64{x, y, z}
	c.yMaxRotSpeed, c.xMaxRotAngle = yMax, xMax
	c.cooldown = 2
}

// LookingAtTarget reports if the control looks at something.
func (c *lookControl) LookingAtTarget() bool { return c.cooldown > 0 }

// Wanted returns the position the control looks at.
func (c *lookControl) Wanted() [3]float64 { return c.wanted }

// tick ports LookControl.tick.
func (c *lookControl) tick(m *Mob) {
	if !c.keepPitch {
		m.SetPitch(0)
	}
	if c.cooldown > 0 {
		c.cooldown--
		if yaw, ok := c.yawD(m); ok {
			m.AI.headYaw = rotateTowards(m.AI.headYaw, yaw, c.yMaxRotSpeed)
		}
		if pitch, ok := c.pitchD(m); ok {
			m.SetPitch(rotateTowards(m.Pitch(), pitch, c.xMaxRotAngle))
		}
	} else {
		m.AI.headYaw = rotateTowards(m.AI.headYaw, m.AI.bodyYaw, 10)
	}
	if !m.AI.nav.IsDone() {
		m.AI.headYaw = rotateIfNecessary(m.AI.headYaw, m.AI.bodyYaw, m.AI.MaxHeadYaw)
	}
}

func (c *lookControl) pitchD(m *Mob) (float64, bool) {
	xd, yd, zd := c.wanted[0]-m.data.Pos[0], c.wanted[1]-m.EyeY(), c.wanted[2]-m.data.Pos[2]
	sd := math.Sqrt(xd*xd + zd*zd)
	if math.Abs(yd) <= 1e-5 && math.Abs(sd) <= 1e-5 {
		return 0, false
	}
	return -(math.Atan2(yd, sd) * 180 / math.Pi), true
}

func (c *lookControl) yawD(m *Mob) (float64, bool) {
	xd, zd := c.wanted[0]-m.data.Pos[0], c.wanted[2]-m.data.Pos[2]
	if math.Abs(zd) <= 1e-5 && math.Abs(xd) <= 1e-5 {
		return 0, false
	}
	return math.Atan2(zd, xd)*180/math.Pi - 90, true
}

// jumpControl makes a mob jump the next tick, ported from Java's
// JumpControl.
type jumpControl struct{ jump bool }

// Jump jumps the mob.
func (c *jumpControl) Jump() { c.jump = true }

func (c *jumpControl) tick(m *Mob) {
	m.AI.jumping = c.jump
	c.jump = false
}

// bodyRotationControl turns the body of a mob after its head and movement,
// ported from Java's BodyRotationControl.
type bodyRotationControl struct {
	headStableTime    int
	lastStableHeadYaw float64
}

func (c *bodyRotationControl) tick(m *Mob) {
	ai := m.AI
	dx, dz := m.data.Pos[0]-ai.prevPos[0], m.data.Pos[2]-ai.prevPos[2]
	if dx*dx+dz*dz > 2.5000003e-7 {
		ai.bodyYaw = m.Yaw()
		ai.headYaw = rotateIfNecessary(ai.headYaw, ai.bodyYaw, ai.MaxHeadYaw)
		c.lastStableHeadYaw = ai.headYaw
		c.headStableTime = 0
		return
	}
	if math.Abs(ai.headYaw-c.lastStableHeadYaw) > 15 {
		c.headStableTime = 0
		c.lastStableHeadYaw = ai.headYaw
		ai.bodyYaw = rotateIfNecessary(ai.bodyYaw, ai.headYaw, ai.MaxHeadYaw)
		return
	}
	c.headStableTime++
	if c.headStableTime > 10 {
		fraction := math.Max(0, math.Min(1, float64(c.headStableTime-10)/10))
		ai.bodyYaw = rotateIfNecessary(ai.bodyYaw, ai.headYaw, ai.MaxHeadYaw*(1-fraction))
	}
}
