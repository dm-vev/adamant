package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports the goals of Java's Rabbit and the hops its move and jump
// controls make it move by.

// registerGoals ports Rabbit.registerGoals and the goals Rabbit.setVariant
// adds to the killer bunny.
func (b *RabbitBehaviour) registerGoals(m *Mob) {
	ai := m.AI
	ai.move.adjustWanted = func(speedModifier float64) float64 {
		// RabbitMoveControl.setWantedPosition: a rabbit swims at 1.5 times
		// its speed and hops at the last speed it wanted to walk at.
		if ai.waterHeight > 0 {
			speedModifier = 1.5
		}
		if speedModifier > 0 {
			b.nextJumpSpeed = speedModifier
		}
		return speedModifier
	}
	ai.moveOverride = rabbitMoveControl{b}
	b.setSpeedModifier(m, 0)

	b.panic = NewPanicGoal(2.2)
	ai.Goals.Add(1, NewFloatGoal(m))
	ai.Goals.Add(1, newClimbOnTopOfPowderSnowGoal())
	ai.Goals.Add(1, rabbitPanicGoal{b.panic, b})
	ai.Goals.Add(2, NewBreedGoal(.8))
	ai.Goals.Add(3, NewTemptGoal(1, rabbitFood, false))
	isWolf := func(e world.Entity) bool { return e.H().Type().EncodeEntity() == "minecraft:wolf" }
	isMonster := func(e world.Entity) bool { return monsterClass[e.H().Type().EncodeEntity()] }
	ai.Goals.Add(4, rabbitAvoidEntityGoal{NewAvoidEntityGoal(isPlayerEntity, 8, 2.2, 2.2), b})
	ai.Goals.Add(4, rabbitAvoidEntityGoal{NewAvoidEntityGoal(isWolf, 10, 2.2, 2.2), b})
	ai.Goals.Add(4, rabbitAvoidEntityGoal{NewAvoidEntityGoal(isMonster, 4, 2.2, 2.2), b})
	ai.Goals.Add(5, &raidGardenGoal{goalFlags: goalFlags{GoalMove | GoalJump}, rabbit: b})
	ai.Goals.Add(6, NewWaterAvoidingRandomStrollGoal(.6))
	ai.Goals.Add(11, NewLookAtPlayerGoal(10))
	if b.killer {
		ai.Goals.Add(4, NewMeleeAttackGoal(1.4, true))
		hurt := NewHurtByTargetGoal()
		hurt.AlertOthers = true
		ai.Targets.Add(1, hurt)
		ai.Targets.Add(2, NewNearestAttackableTargetGoal(isPlayerEntity, true))
		ai.Targets.Add(2, NewNearestAttackableTargetGoal(isWolf, true))
	}
}

// setSpeedModifier ports Rabbit.setSpeedModifier: it sets the speed of the
// path the rabbit follows and wants to walk at.
func (b *RabbitBehaviour) setSpeedModifier(m *Mob, speed float64) {
	m.AI.nav.SetSpeedModifier(speed)
	w := m.AI.move.Wanted()
	m.AI.move.SetWantedPosition(w[0], w[1], w[2], speed)
}

// setJumping ports Rabbit.setJumping, which plays the hop sound when the
// rabbit jumps.
func (b *RabbitBehaviour) setJumping(m *Mob, jump bool) {
	m.AI.jumping = jump
	if jump {
		r := m.Random()
		m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.rabbit.jump", Volume: 1, Pitch: float64(((r.NextFloat()-r.NextFloat())*.2 + 1) * .8)})
	}
}

// startJumping ports Rabbit.startJumping: a hop lasts 15 ticks.
func (b *RabbitBehaviour) startJumping(m *Mob) {
	b.setJumping(m, true)
	b.jumpDuration, b.jumpTicks = 15, 0
}

// facePoint ports Rabbit.facePoint.
func (b *RabbitBehaviour) facePoint(m *Mob, x, z float64) {
	m.SetYaw(math.Atan2(z-m.data.Pos[2], x-m.data.Pos[0])*180/math.Pi - 90)
}

// tickJumpControl ports RabbitJumpControl.tick: a jump the rabbit wants
// starts a hop.
func (b *RabbitBehaviour) tickJumpControl(m *Mob) {
	if m.AI.jump.jump {
		b.startJumping(m)
		m.AI.jump.jump = false
	}
}

// customServerAiStep ports Rabbit.customServerAiStep: on the ground the
// rabbit waits out its landing delay, then hops towards where it wants to
// go, and the killer bunny hops at a target within four blocks.
func (b *RabbitBehaviour) customServerAiStep(m *Mob) {
	if b.jumpDelayTicks > 0 {
		b.jumpDelayTicks--
	}
	if b.moreCarrotTicks > 0 {
		b.moreCarrotTicks = max(0, b.moreCarrotTicks-int(m.Random().NextInt(3)))
	}
	if m.OnGround() {
		if !b.wasOnGround {
			b.setJumping(m, false)
			// Rabbit.checkLandingDelay: a panicking rabbit hops again sooner.
			b.jumpDelayTicks = 10
			if m.AI.move.SpeedModifier() >= 2.2 {
				b.jumpDelayTicks = 3
			}
		}
		if b.killer && b.jumpDelayTicks == 0 {
			if target := m.Target(); target != nil && m.DistanceToSqr(target) < 16 {
				pos := target.Position()
				b.facePoint(m, pos[0], pos[2])
				m.AI.move.SetWantedPosition(pos[0], pos[1], pos[2], m.AI.move.SpeedModifier())
				b.startJumping(m)
				b.wasOnGround = true
			}
		}
		if !m.AI.jump.jump && m.AI.move.HasWanted() && b.jumpDelayTicks == 0 {
			pos := mgl64.Vec3(m.AI.move.Wanted())
			if p := m.AI.nav.path; p != nil && !p.Done() {
				pos = p.entityPosAtNode(m.Width(), p.next)
			}
			b.facePoint(m, pos[0], pos[2])
			b.startJumping(m)
		}
	}
	b.wasOnGround = m.OnGround()
}

// jumpFromGround ports Rabbit.jumpFromGround and getJumpPower: a rabbit hops
// higher while it runs or jumps onto a block, and a rabbit that stands still
// pushes off forwards.
func (b *RabbitBehaviour) jumpFromGround(m *Mob) bool {
	ai := m.AI
	power := float32(.3)
	if ai.move.SpeedModifier() <= .6 {
		power = .2
	}
	if p := ai.nav.path; p != nil && !p.Done() && p.entityPosAtNode(m.Width(), p.next)[1] > m.data.Pos[1]+.5 {
		power = .5
	}
	if ai.collidedHorizontally || ai.jumping && ai.move.Wanted()[1] > m.data.Pos[1]+.5 {
		power = .5
	}
	jump := float64(float32(ai.JumpStrength)*(power/.42)*float32(blockJumpFactor(m))) + jumpBoostPower(m)
	if jump > 1e-5 {
		m.data.Vel[1] = math.Max(jump, m.data.Vel[1])
	}
	if ai.move.SpeedModifier() > 0 && m.data.Vel[0]*m.data.Vel[0]+m.data.Vel[2]*m.data.Vel[2] < .01 {
		height := 1.5
		if b.age < 0 {
			height = .5
		}
		ai.moveRelative(m, .1, mgl64.Vec3{0, height, 1})
	}
	return true
}

func (b *RabbitBehaviour) jumpInLiquid(*Mob, bool) bool { return false }

// rabbitMoveControl ports RabbitMoveControl: a rabbit on the ground stands
// still until it hops, and moves at the speed it wanted while it does.
type rabbitMoveControl struct{ rabbit *RabbitBehaviour }

func (c rabbitMoveControl) tick(m *Mob) {
	b := c.rabbit
	switch {
	case m.OnGround() && !m.AI.jumping && !m.AI.jump.jump:
		b.setSpeedModifier(m, 0)
	case m.AI.move.HasWanted() || m.AI.move.operation == moveJumping:
		b.setSpeedModifier(m, b.nextJumpSpeed)
	}
	m.AI.move.tick(m)
}

// rabbitPanicGoal ports RabbitPanicGoal: a panicking rabbit keeps hopping
// at its panic speed.
type rabbitPanicGoal struct {
	*PanicGoal
	rabbit *RabbitBehaviour
}

func (g rabbitPanicGoal) Tick(m *Mob) { g.rabbit.setSpeedModifier(m, g.SpeedModifier) }

// rabbitAvoidEntityGoal ports RabbitAvoidEntityGoal: the killer bunny flees
// from nothing.
type rabbitAvoidEntityGoal struct {
	*AvoidEntityGoal
	rabbit *RabbitBehaviour
}

func (g rabbitAvoidEntityGoal) CanUse(m *Mob) bool {
	return !g.rabbit.killer && g.AvoidEntityGoal.CanUse(m)
}

// raidGardenGoal ports Rabbit.RaidGardenGoal: a rabbit that has not eaten
// for a while walks to a fully grown carrot within 16 blocks and nibbles it
// down a stage once it stands on it.
type raidGardenGoal struct {
	goalFlags
	moveToBlock
	rabbit               *RabbitBehaviour
	wantsToRaid, canRaid bool
}

func (g *raidGardenGoal) CanUse(m *Mob) bool {
	if g.nextStartTick <= 0 {
		if mobGriefingDisabled(m.tx) {
			return false
		}
		g.canRaid = false
		g.wantsToRaid = g.rabbit.moreCarrotTicks <= 0
	}
	return g.moveToBlock.canUse(m, 16, 1, g.validTarget)
}

func (g *raidGardenGoal) CanContinueToUse(m *Mob) bool {
	return g.canRaid && g.moveToBlock.canContinueToUse(m, g.validTarget)
}

func (g *raidGardenGoal) RequiresUpdateEveryTick() bool { return true }

func (g *raidGardenGoal) Start(m *Mob) { g.moveToBlock.start(m, .7) }

func (g *raidGardenGoal) Tick(m *Mob) {
	g.moveToBlock.tick(m, .7)
	m.AI.look.SetLookAtWith(float64(g.pos[0])+.5, float64(g.pos[1]+1), float64(g.pos[2])+.5, 10, m.AI.MaxHeadPitch)
	if !g.reached {
		return
	}
	crops := g.pos.Side(cube.FaceUp)
	if carrot, ok := m.tx.Block(crops).(block.Carrot); ok && g.canRaid {
		if carrot.Growth == 0 {
			m.tx.SetBlock(crops, nil, nil)
		} else {
			m.tx.AddParticle(crops.Vec3Centre(), particle.BlockBreak{Block: carrot})
			carrot.Growth--
			m.tx.SetBlock(crops, carrot, nil)
			m.tx.EmitGameEvent(world.GameEventBlockChange, crops.Vec3Centre(), world.GameEventContext{Source: m.Self})
		}
		g.rabbit.moreCarrotTicks = 40
	}
	g.canRaid = false
	g.nextStartTick = 10
}

// validTarget ports RaidGardenGoal.isValidTarget: farmland under a fully
// grown carrot, while the rabbit wants food and has found none yet.
func (g *raidGardenGoal) validTarget(m *Mob, pos cube.Pos) bool {
	if !blockInTag(m.tx.Block(pos), "minecraft:supports_crops") || !g.wantsToRaid || g.canRaid {
		return false
	}
	if carrot, ok := m.tx.Block(pos.Side(cube.FaceUp)).(block.Carrot); ok && carrot.Growth == 7 {
		g.canRaid = true
		return true
	}
	return false
}

// climbOnTopOfPowderSnowGoal makes a mob that walks on powder snow jump out
// of powder snow it sank into, ported from Java's
// ClimbOnTopOfPowderSnowGoal.
type climbOnTopOfPowderSnowGoal struct{ goalFlags }

func newClimbOnTopOfPowderSnowGoal() *climbOnTopOfPowderSnowGoal {
	return &climbOnTopOfPowderSnowGoal{goalFlags{GoalJump}}
}

func (g *climbOnTopOfPowderSnowGoal) CanUse(m *Mob) bool {
	if !m.data.InPowderSnow && !m.data.WasInPowderSnow || !entityTypeInTag(m.Self, "minecraft:powder_snow_walkable_mobs") {
		return false
	}
	above := m.BlockPos().Side(cube.FaceUp)
	b := m.tx.Block(above)
	if _, ok := b.(block.PowderSnow); ok {
		return true
	}
	return len(b.Model().BBox(above, m.tx)) == 0
}

func (g *climbOnTopOfPowderSnowGoal) RequiresUpdateEveryTick() bool { return true }

func (g *climbOnTopOfPowderSnowGoal) Tick(m *Mob) { m.AI.jump.Jump() }
