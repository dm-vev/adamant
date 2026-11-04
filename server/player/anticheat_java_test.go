package player

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// javaMover moves a player as Java's LivingEntity.travel, aiStep and
// Player.jumpFromGround do, against the blocks of a world: the client the
// false-positive suites stand in for. It is written apart from the
// envelopes of package anticheat, from the decompiled classes, so that a
// mistake in one does not hide in the other.
//
// It ports travelInAir with the friction, speed and jump factors of blocks,
// travelInFluid in water, bubble columns, climbing (ladders, vines,
// scaffolding), Levitation and Slow Falling, the step-up of 0.6, the slime
// bounce and slowdown, and sneaking's backing off edges.
type javaMover struct {
	pos, vel            mgl64.Vec3
	onGround            bool
	horizontalCollision bool
	// pending are velocities the server gave, which the client applies at
	// a tick of its own: one round trip later.
	pending []javaImpulse
	tick    int
	mc      entity.MovementComputer
	// acknowledge is set when the server teleported the player: the next
	// move acknowledges it.
	acknowledge bool
}

// javaImpulse is a velocity the client applies at a tick.
type javaImpulse struct {
	at  int
	vel mgl64.Vec3
	add bool
}

// javaInput is what the client holds down in a tick.
type javaInput struct {
	// forward and strafe are the movement input, from -1 to 1, before
	// Java's scaling by 0.98.
	forward, strafe float64
	yaw             float64
	sprint, jump    bool
	sneak           bool
	// speed is the movement speed attribute, 0.1 if 0; sprinting adds 30%.
	speed       float64
	levitation  int
	slowFalling bool
	jumpBoost   int
	// dolphin is Dolphin's Grace.
	dolphin bool
}

// apply has the client apply a velocity at its tick at, set or added.
func (j *javaMover) apply(at int, vel mgl64.Vec3, add bool) {
	j.pending = append(j.pending, javaImpulse{at: at, vel: vel, add: add})
}

// step moves the player a tick and returns where it is.
func (j *javaMover) step(tx *world.Tx, body world.Entity, in javaInput) mgl64.Vec3 {
	j.tick++
	kept := j.pending[:0]
	for _, i := range j.pending {
		switch {
		case i.at > j.tick:
			kept = append(kept, i)
		case i.add:
			j.vel = j.vel.Add(i.vel)
		default:
			j.vel = i.vel
		}
	}
	j.pending = kept

	speed := in.speed
	if speed == 0 {
		speed = 0.1
	}
	if in.sprint {
		speed *= 1.3
	}
	strafe, forward := in.strafe*0.98, in.forward*0.98
	if in.sneak {
		// Player's sneaking speed modifier of 0.3.
		strafe, forward = strafe*0.3, forward*0.3
	}
	water := j.inWater(tx)
	if in.jump {
		switch {
		case water:
			// LivingEntity.jumpInLiquid.
			j.vel[1] += 0.04
		case j.onGround:
			j.jumpFromGround(tx, in)
		}
	}
	if water {
		j.travelInWater(tx, body, in, strafe, forward)
	} else {
		j.travelInAir(tx, body, in, speed, strafe, forward)
	}
	return j.pos
}

// jumpFromGround ports LivingEntity.jumpFromGround and the sprint-jump of
// Player.
func (j *javaMover) jumpFromGround(tx *world.Tx, in javaInput) {
	power := 0.42 * j.jumpFactor(tx)
	j.vel[1] = max(power+0.1*float64(in.jumpBoost), j.vel[1])
	if in.sprint {
		rad := in.yaw * math.Pi / 180
		j.vel[0] += -math.Sin(rad) * 0.2
		j.vel[2] += math.Cos(rad) * 0.2
	}
}

// below returns the block below the position that affects the movement of
// the player, Entity.getBlockPosBelowThatAffectsMyMovement.
func (j *javaMover) below(tx *world.Tx) world.Block {
	return tx.Block(cube.PosFromVec3(j.pos.Sub(mgl64.Vec3{0, 0.500001, 0})))
}

// friction returns the friction of the block below.
func (j *javaMover) friction(tx *world.Tx) float64 {
	switch j.below(tx).(type) {
	case block.Ice, block.PackedIce:
		return 0.98
	case block.BlueIce:
		return 0.989
	case block.Slime:
		return 0.8
	}
	return 0.6
}

// speedFactor ports Entity.getBlockSpeedFactor: soul sand and honey slow
// walking to 0.4, the block the feet are in first, unless water.
func (j *javaMover) speedFactor(tx *world.Tx) float64 {
	in := tx.Block(cube.PosFromVec3(j.pos))
	if _, ok := in.(block.Air); !ok {
		if f, ok := blockSpeedFactor(in); ok {
			return f
		}
	}
	if f, ok := blockSpeedFactor(j.below(tx)); ok {
		return f
	}
	return 1
}

func blockSpeedFactor(b world.Block) (float64, bool) {
	switch b.(type) {
	case block.SoulSand, block.HoneyBlock:
		return 0.4, true
	}
	return 0, false
}

// jumpFactor ports Entity.getBlockJumpFactor: honey halves jumps.
func (j *javaMover) jumpFactor(tx *world.Tx) float64 {
	if _, ok := tx.Block(cube.PosFromVec3(j.pos)).(block.HoneyBlock); ok {
		return 0.5
	}
	if _, ok := j.below(tx).(block.HoneyBlock); ok {
		return 0.5
	}
	return 1
}

// inWater reports whether the feet or the body of the player are in water.
func (j *javaMover) inWater(tx *world.Tx) bool {
	for _, dy := range []float64{0.01, 0.9} {
		if l, ok := tx.Liquid(cube.PosFromVec3(j.pos.Add(mgl64.Vec3{0, dy, 0}))); ok && l.LiquidType() == "water" {
			return true
		}
	}
	return false
}

// onClimbable ports LivingEntity.onClimbable for ladders, vines and
// scaffolding.
func (j *javaMover) onClimbable(tx *world.Tx) bool {
	switch tx.Block(cube.PosFromVec3(j.pos)).(type) {
	case block.Ladder, block.Vines, block.Scaffolding:
		return true
	}
	return false
}

// moveRelative ports Entity.moveRelative.
func (j *javaMover) moveRelative(speed, strafe, forward, yaw float64) {
	l := strafe*strafe + forward*forward
	if l < 1e-7 {
		return
	}
	if l > 1 {
		n := math.Sqrt(l)
		strafe, forward = strafe/n, forward/n
	}
	strafe, forward = strafe*speed, forward*speed
	rad := yaw * math.Pi / 180
	sin, cos := math.Sin(rad), math.Cos(rad)
	j.vel[0] += strafe*cos - forward*sin
	j.vel[2] += forward*cos + strafe*sin
}

// travelInAir ports LivingEntity.travelInAir and
// handleRelativeFrictionAndCalculateMovement.
func (j *javaMover) travelInAir(tx *world.Tx, body world.Entity, in javaInput, speed, strafe, forward float64) {
	f := 1.0
	if j.onGround {
		f = j.friction(tx)
	}
	f1 := f * 0.91
	accel := 0.02
	if in.sprint {
		accel = 0.025999999
	}
	if j.onGround {
		accel = speed * (0.21600002 / (f * f * f))
	}
	j.moveRelative(accel, strafe, forward, in.yaw)
	climbing := j.onClimbable(tx)
	if climbing {
		// LivingEntity.handleOnClimbable.
		j.vel[0] = max(-0.15, min(0.15, j.vel[0]))
		j.vel[2] = max(-0.15, min(0.15, j.vel[2]))
		j.vel[1] = max(j.vel[1], -0.15)
		if in.sneak && j.vel[1] < 0 {
			j.vel[1] = 0
		}
	}
	j.move(tx, body, in)
	if (j.horizontalCollision || in.jump) && climbing {
		j.vel[1] = 0.2
	}
	vy := j.vel[1]
	if in.levitation > 0 {
		vy += (0.05*float64(in.levitation) - vy) * 0.2
	} else {
		g := 0.08
		if in.slowFalling && vy <= 0 {
			g = 0.01
		}
		vy -= g
	}
	j.vel = mgl64.Vec3{j.vel[0] * f1, vy * 0.98, j.vel[2] * f1}
}

// travelInWater ports LivingEntity.travelInFluid in water.
func (j *javaMover) travelInWater(tx *world.Tx, body world.Entity, in javaInput, strafe, forward float64) {
	f := 0.8
	if in.sprint {
		f = 0.9
	}
	if in.dolphin {
		f = 0.96
	}
	j.moveRelative(0.02, strafe, forward, in.yaw)
	j.move(tx, body, in)
	if j.horizontalCollision && j.onClimbable(tx) {
		j.vel[1] = 0.2
	}
	j.vel = mgl64.Vec3{j.vel[0] * f, j.vel[1] * 0.8, j.vel[2] * f}
	if !in.sprint {
		// getFluidFallingAdjustedMovement: gravity / 16.
		j.vel[1] -= 0.08 / 16
	}
}

// move ports Entity.move: the collisions, with the step-up of a player on
// the ground, sneaking's backing off edges, the blocks that change the
// velocity after a fall and inside them, and the speed factor.
func (j *javaMover) move(tx *world.Tx, body world.Entity, in javaInput) {
	want := j.vel
	if in.sneak && j.onGround {
		want = j.backOffFromEdge(tx, body, want)
	}
	got, _ := j.mc.CheckCollision(tx, body, j.pos, want)
	collidedY := got[1] != want[1]
	if (j.onGround || collidedY && want[1] < 0) && (got[0] != want[0] || got[2] != want[2]) {
		// Entity.collide: step up as high as 0.6 if that goes farther.
		up, _ := j.mc.CheckCollision(tx, body, j.pos, mgl64.Vec3{0, 0.6, 0})
		across, _ := j.mc.CheckCollision(tx, body, j.pos.Add(up), mgl64.Vec3{want[0], 0, want[2]})
		down, _ := j.mc.CheckCollision(tx, body, j.pos.Add(up).Add(across), mgl64.Vec3{0, -up[1] + min(want[1], 0), 0})
		stepped := up.Add(across).Add(down)
		if stepped[0]*stepped[0]+stepped[2]*stepped[2] > got[0]*got[0]+got[2]*got[2]+1e-9 {
			got, collidedY = stepped, true
		}
	}
	j.pos = j.pos.Add(got)
	j.horizontalCollision = math.Abs(got[0]-want[0]) > 1e-7 || math.Abs(got[2]-want[2]) > 1e-7
	j.onGround = collidedY && want[1] < 0
	if math.Abs(got[0]-want[0]) > 1e-7 {
		j.vel[0] = 0
	}
	if math.Abs(got[2]-want[2]) > 1e-7 {
		j.vel[2] = 0
	}
	if collidedY {
		// Block.updateEntityMovementAfterFallOn: slime bounces a player that
		// does not sneak, as high as it fell; others stop it.
		if _, slime := j.below(tx).(block.Slime); slime && want[1] < 0 && !in.sneak {
			j.vel[1] = -want[1]
		} else {
			j.vel[1] = 0
		}
	}
	if _, slime := j.below(tx).(block.Slime); slime && j.onGround && math.Abs(j.vel[1]) < 0.1 && !in.sneak {
		// SlimeBlock.stepOn.
		d := 0.4 + math.Abs(j.vel[1])*0.2
		j.vel[0], j.vel[2] = j.vel[0]*d, j.vel[2]*d
	}
	j.insideBlocks(tx)
	sf := j.speedFactor(tx)
	j.vel[0], j.vel[2] = j.vel[0]*sf, j.vel[2]*sf
}

// insideBlocks ports what the blocks the player is in do to its velocity:
// bubble columns lift it or drag it down, cobwebs and powder snow slow it.
func (j *javaMover) insideBlocks(tx *world.Tx) {
	feet := cube.PosFromVec3(j.pos)
	switch b := tx.Block(feet).(type) {
	case block.BubbleColumn:
		_, airAbove := tx.Block(feet.Side(cube.FaceUp)).(block.Air)
		switch {
		case b.DragDown && airAbove:
			j.vel[1] = max(-0.9, j.vel[1]-0.03)
		case b.DragDown:
			j.vel[1] = max(-0.3, j.vel[1]-0.03)
		case airAbove:
			j.vel[1] = min(1.8, j.vel[1]+0.1)
		default:
			j.vel[1] = min(0.7, j.vel[1]+0.06)
		}
	case block.Cobweb:
		j.vel = mgl64.Vec3{j.vel[0] * 0.25, j.vel[1] * 0.05, j.vel[2] * 0.25}
	case block.PowderSnow:
		j.vel = mgl64.Vec3{j.vel[0] * 0.9, j.vel[1] * 1.5, j.vel[2] * 0.9}
	}
}

// backOffFromEdge ports Player.maybeBackOffFromEdge: a sneaking player on
// the ground does not step off a block.
func (j *javaMover) backOffFromEdge(tx *world.Tx, body world.Entity, v mgl64.Vec3) mgl64.Vec3 {
	supported := func(dx, dz float64) bool {
		down, _ := j.mc.CheckCollision(tx, body, j.pos.Add(mgl64.Vec3{dx, 0, dz}), mgl64.Vec3{0, -0.6, 0})
		return down[1] > -0.6+1e-7
	}
	const step = 0.05
	for v[0] != 0 && !supported(v[0], 0) {
		if math.Abs(v[0]) < step {
			v[0] = 0
		} else {
			v[0] -= math.Copysign(step, v[0])
		}
	}
	for v[2] != 0 && !supported(v[0], v[2]) {
		if math.Abs(v[2]) < step {
			v[2] = 0
		} else {
			v[2] -= math.Copysign(step, v[2])
		}
	}
	return v
}
