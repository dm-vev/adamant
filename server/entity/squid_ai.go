package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports the goals and movement of Java's Squid and GlowSquid.

// squidState is the movement state of a squid: how far its tentacles moved,
// how fast they move and turn, the push it swims with and the pitch of its
// body.
type squidState struct {
	tentacleMovement, tentacleSpeed, rotateSpeed float32
	movementVector                               mgl64.Vec3
	xBodyRot                                     float64
	started                                      bool
}

// squidKind reports if the wildlife is a squid or glow squid.
func (b *oceanWildlifeBehaviour) squidKind() bool {
	return b.id == "minecraft:squid" || b.id == "minecraft:glow_squid"
}

// mobAI returns the MobAI of a squid or dolphin, or nil for the turtles
// that do not run on one.
func (b *oceanWildlifeBehaviour) mobAI() *MobAI {
	if b.id == "minecraft:turtle" {
		return nil
	}
	return &b.ai
}

// tickSquid runs a tick of a squid: its age and air and its MobAI.
func (b *oceanWildlifeBehaviour) tickSquid(e *Ent, tx *world.Tx) *Movement {
	self := oceanWildlifeEntity(e)
	ai := &b.ai
	ai.EyeHeight = .4
	if b.age < 0 {
		ai.EyeHeight = .37
	}
	m := ai.Mob(e, self)
	if !b.squid.started {
		// The constructor of Squid picks how fast its tentacles move.
		b.squid.started, b.squid.tentacleSpeed = true, 1/(m.Random().NextFloat()+1)*.2
	}
	// AgeableMob.aiStep: a baby squid grows up and an adult's breeding
	// cooldown runs out.
	if b.age < 0 {
		b.age++
	} else if b.age > 0 {
		b.age--
	}
	// AgeableWaterCreature.handleAirSupply: a squid out of water drowns
	// after 15 seconds.
	if _, inWater := tx.Liquid(cube.PosFromVec3(e.data.Pos)); inWater {
		b.air = 300
	} else if b.air--; b.air <= -20 {
		b.air = 0
		self.(Living).Hurt(2, DrowningDamageSource{})
		if b.dead {
			return nil
		}
	}
	return ai.Tick(e, self, b.registerSquidGoals)
}

// oceanWildlifeEntity returns the species entity of wildlife.
func oceanWildlifeEntity(e *Ent) world.Entity {
	base := &oceanWildlife{Ent: e}
	switch base.b().id {
	case "minecraft:squid":
		return &Squid{base}
	case "minecraft:glow_squid":
		return &GlowSquid{base}
	case "minecraft:turtle":
		return &Turtle{base}
	}
	return &Dolphin{base}
}

// registerSquidGoals ports Squid.registerGoals.
func (b *oceanWildlifeBehaviour) registerSquidGoals(m *Mob) {
	m.AI.SetPathfindingMalus(PathWater, 0)
	m.AI.Gravity = .08
	m.AI.Goals.Add(0, &squidRandomMovementGoal{squid: b})
	m.AI.Goals.Add(1, &squidFleeGoal{squid: b})
}

// travel ports Squid.travel: a squid moves only by the push its tentacles
// give it.
func (b *oceanWildlifeBehaviour) travel(m *Mob, input mgl64.Vec3) bool {
	if b.id == "minecraft:dolphin" {
		return b.travelDolphin(m, input)
	}
	m.AI.moveSelf(m, m.data.Vel)
	return true
}

// afterAiStep ports what Squid.aiStep does after that of LivingEntity: the
// tentacles of a squid in water push it along its movement vector once a
// stroke, and it drifts in between, and a squid out of water falls. A glow
// squid's darkness wears off.
func (b *oceanWildlifeBehaviour) afterAiStep(m *Mob) {
	if !b.squidKind() {
		return
	}
	s := &b.squid
	s.tentacleMovement += s.tentacleSpeed
	if s.tentacleMovement > math.Pi*2 {
		s.tentacleMovement -= math.Pi * 2
		if m.Random().NextInt(10) == 0 {
			s.tentacleSpeed = 1 / (m.Random().NextFloat() + 1) * .2
		}
	}
	if m.InWater() {
		if s.tentacleMovement < math.Pi {
			if s.tentacleMovement/math.Pi > .75 {
				m.data.Vel = s.movementVector
				s.rotateSpeed = 1
			} else {
				s.rotateSpeed *= .8
			}
		} else {
			m.data.Vel = m.data.Vel.Mul(.9)
			s.rotateSpeed *= .99
		}
		v := m.data.Vel
		body := m.AI.bodyYaw + (-math.Atan2(v[0], v[2])*180/math.Pi-m.AI.bodyYaw)*.1
		m.AI.bodyYaw = body
		m.SetYaw(body)
		s.xBodyRot += (-math.Atan2(math.Hypot(v[0], v[2]), v[1])*180/math.Pi - s.xBodyRot) * .1
	} else {
		yd := m.data.Vel[1]
		if levitation, ok := mobEffect(m, effect.Levitation); ok {
			yd = .05 * float64(levitation.Level())
		} else {
			yd -= m.AI.Gravity
		}
		m.data.Vel = mgl64.Vec3{0, yd * .98, 0}
		s.xBodyRot += (-90 - s.xBodyRot) * .02
	}
	if b.id == "minecraft:glow_squid" && b.darkTicks > 0 {
		b.darkTicks--
	}
}

// squidRandomMovementGoal ports SquidRandomMovementGoal: now and then, and
// whenever it is out of water or still, a squid picks a new direction to
// swim in, but it stops when idle for five seconds.
type squidRandomMovementGoal struct {
	goalFlags
	squid *oceanWildlifeBehaviour
}

func (g *squidRandomMovementGoal) CanUse(*Mob) bool { return true }

func (g *squidRandomMovementGoal) Tick(m *Mob) {
	s := &g.squid.squid
	if m.NoActionTime() > 100 {
		s.movementVector = mgl64.Vec3{}
		return
	}
	r := m.Random()
	if r.NextInt(int32(reducedTickDelay(50))) == 0 || !m.InWater() || s.movementVector.LenSqr() <= float64(float32(1e-5)) {
		angle := r.NextFloat() * math.Pi * 2
		s.movementVector = mgl64.Vec3{float64(float32(math.Cos(float64(angle))) * .2), float64(-.1 + r.NextFloat()*.2), float64(float32(math.Sin(float64(angle))) * .2)}
	}
}

// squidFleeGoal ports SquidFleeGoal: a squid in water swims away from what
// hurt it while it is within ten blocks, faster the closer it is.
type squidFleeGoal struct {
	goalFlags
	squid     *oceanWildlifeBehaviour
	fleeTicks int
}

func (g *squidFleeGoal) CanUse(m *Mob) bool {
	attacker, _ := m.LastHurtBy()
	return m.InWater() && attacker != nil && m.DistanceToSqr(attacker) < 100
}

func (g *squidFleeGoal) RequiresUpdateEveryTick() bool { return true }

func (g *squidFleeGoal) Start(*Mob) { g.fleeTicks = 0 }

func (g *squidFleeGoal) Tick(m *Mob) {
	g.fleeTicks++
	attacker, _ := m.LastHurtBy()
	if attacker == nil {
		return
	}
	fleeTo := m.data.Pos.Sub(attacker.Position())
	pos := cube.PosFromVec3(m.data.Pos.Add(fleeTo))
	air := isAir(pathBlockAt(m.tx, pos))
	if !pathWater(m.tx, pos) && !air {
		return
	}
	// Java normalises the vector without keeping the result, so the squid
	// flees at the distance it is from its attacker scaled.
	if length := fleeTo.Len(); length > 0 {
		avoidSpeed := 3.0
		if length > 5 {
			avoidSpeed -= (length - 5) / 5
		}
		if avoidSpeed > 0 {
			fleeTo = fleeTo.Mul(avoidSpeed)
		}
	}
	if air {
		fleeTo[1] = 0
	}
	g.squid.squid.movementVector = fleeTo.Mul(1.0 / 20)
}
