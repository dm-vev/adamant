package anticheat

import (
	"math"

	"github.com/go-gl/mathgl/mgl64"
)

// Java's numbers the envelopes are made of: LivingEntity.travel,
// Player.jumpFromGround and the friction of blocks.
const (
	// DefaultFriction is the friction of a block that has none of its own.
	DefaultFriction = 0.6
	// AirDrag is what the horizontal velocity keeps each tick in the air.
	AirDrag = 0.91
	// LiquidDrag is the most the horizontal velocity keeps each tick in a
	// liquid: Dolphin's Grace.
	LiquidDrag = 0.96
	// LiquidAcceleration is the least the envelope grants a tick in a
	// liquid: Depth Strider at its strongest gives the speed of the player.
	LiquidAcceleration = 0.1
	// AirAcceleration is the flying speed of a sprinting player: what it
	// gains a tick in the air.
	AirAcceleration = 0.026
	// SprintJumpBoost is the push a sprint-jump gives.
	SprintJumpBoost = 0.2
	// SprintFactor is what sprinting multiplies the speed by.
	SprintFactor = 1.3
	// Gravity is the acceleration of gravity, and SlowFallingGravity that
	// under Slow Falling.
	Gravity, SlowFallingGravity = 0.08, 0.01
	// VerticalDrag is what the vertical velocity keeps each tick.
	VerticalDrag = 0.98
	// JumpPower is the vertical velocity of a jump, and JumpBoostPower what
	// a level of Jump Boost adds to it.
	JumpPower, JumpBoostPower = 0.42, 0.1
	// ClimbSpeed is the speed of climbing a ladder or a vine.
	ClimbSpeed = 0.2
	// LiquidRise is the most a player rises a tick in a liquid, and
	// BubbleRise the most an upward bubble column throws it.
	LiquidRise, BubbleRise = 0.7, 1.8
	// LevitationPower is the speed a level of Levitation lifts at.
	LevitationPower = 0.05
)

// State is what an envelope of a move is made of: what the server knows of
// the player before the move, and of the move before it.
type State struct {
	// Speed is the movement speed of the player, sprinting and effects
	// included, the most of it over the round trip to its client.
	Speed float64
	// MaySprint is set if the player may sprint but does not: a client
	// starts sprinting in the move it tells the server of it.
	MaySprint bool
	// Ground is set if the player stood on the ground before the move, and
	// Friction is the friction of the block it stood on.
	Ground   bool
	Friction float64
	// Liquid is set if the player was in a liquid before the move.
	Liquid bool
	// Rises is set if the move goes up.
	Rises bool

	// PrevHorizontal is the horizontal length of the move before, 0 after
	// a setback. PrevGround, PrevFriction and PrevLiquid are Ground,
	// Friction and Liquid before the move before.
	PrevHorizontal float64
	PrevGround     bool
	PrevFriction   float64
	PrevLiquid     bool

	// PrevVertical is the vertical part of the move before.
	PrevVertical float64
	// Climbing is set if the player was on a ladder, a vine, scaffolding,
	// or in powder snow it may climb, before the move.
	Climbing bool
	// Bubbles is set if the player was in or on an upward bubble column.
	Bubbles bool
	// JumpBoost and Levitation are the levels of those effects, the most
	// over the round trip to the client.
	JumpBoost, Levitation int
	// JumpFactor is what the block below scales a jump by: 0.5 on honey, 1
	// on most blocks.
	JumpFactor float64
	// SlowFalling is set if the player falls slowly.
	SlowFalling bool
	// Bounce is the speed a bouncy block below throws the player up at,
	// from the speed it landed with.
	Bounce float64
	// Step is the height the player steps up on the ground in a move.
	Step float64

	// Impulse is the largest impulse the server gave the player that its
	// client may still apply: knockback, explosions and the like.
	Impulse mgl64.Vec3
}

// friction returns f, or DefaultFriction if f is not set.
func friction(f float64) float64 {
	if f <= 0 {
		return DefaultFriction
	}
	return f
}

// GroundAcceleration returns what a player of the speed passed gains a tick
// on a block of the friction passed: Java's speed × 0.216 / f³, with the
// most input a client gives, a length of 1.
func GroundAcceleration(speed, f float64) float64 {
	f = friction(f)
	return speed * 0.21600002 / (f * f * f)
}

// HorizontalAllowance returns the most a player of state s moves
// horizontally in a move, with no tolerance: what it carries of the move
// before, what it gains in the move, a sprint-jump and an impulse.
//
// A move keeps of the one before what the friction of where that one began
// leaves: the block below times the air drag on the ground, the air drag in
// the air, and in a liquid at most what Dolphin's Grace keeps. A move gains
// the ground acceleration if the player stood on the ground before it or
// before the move before, a tick of slack around a jump and a landing; in a
// liquid at least LiquidAcceleration; in the air, AirAcceleration.
//
// The friction of a block may differ between a client and the server, as
// honey's does between Java and Bedrock: the envelope also grants what the
// move would be on blocks of the default friction, which accelerate
// quicker and carry less.
func HorizontalAllowance(s State) float64 {
	ordinary := s
	ordinary.Friction, ordinary.PrevFriction = DefaultFriction, DefaultFriction
	return max(horizontalAllowance(s), horizontalAllowance(ordinary))
}

func horizontalAllowance(s State) float64 {
	base := s.Speed
	if s.MaySprint {
		base *= SprintFactor
	}
	ground := GroundAcceleration(base, s.Friction)
	accel := AirAcceleration
	switch {
	case s.Liquid:
		accel = max(LiquidAcceleration, ground)
	case s.Ground || s.PrevGround:
		accel = max(ground, AirAcceleration)
	}
	carry := AirDrag
	switch {
	case s.PrevLiquid:
		carry = LiquidDrag
	case s.PrevGround:
		carry = friction(s.PrevFriction) * AirDrag
	}
	jump := 0.0
	if s.Ground && s.Rises {
		jump = SprintJumpBoost
	}
	return s.PrevHorizontal*carry + accel + jump + math.Hypot(s.Impulse[0], s.Impulse[2])
}

// Supported reports whether something holds the player up in state s: the
// ground, a climbable block, a liquid or a bounce.
func (s State) Supported() bool {
	return s.Ground || s.Climbing || s.Liquid || s.Bubbles || s.Bounce > 0
}

// AirborneVertical returns the vertical move gravity leaves a player of
// state s that nothing holds up: Java's (vy − g) × 0.98 from the move
// before, which may be negative.
func AirborneVertical(s State) float64 {
	g := Gravity
	if s.SlowFalling {
		g = SlowFallingGravity
	}
	vy := (s.PrevVertical - g) * VerticalDrag
	if s.Levitation > 0 {
		vy = max(vy, s.PrevVertical, LevitationPower*float64(s.Levitation))
	}
	return vy
}

// VerticalAllowance returns the most a player of state s rises in a move,
// with no epsilon: in the air what gravity leaves of the move before, and,
// held up, the most of a jump, a step, a climb, a rise in a liquid and a
// bounce; with an impulse on top.
func VerticalAllowance(s State) float64 {
	up := AirborneVertical(s)
	if s.Supported() {
		factor := s.JumpFactor
		if factor <= 0 {
			factor = 1
		}
		up = max(up, JumpPower*factor+JumpBoostPower*float64(s.JumpBoost), s.Step, s.Bounce)
		if s.Climbing {
			up = max(up, ClimbSpeed)
		}
		if s.Liquid {
			up = max(up, LiquidRise)
		}
		if s.Bubbles {
			up = max(up, BubbleRise)
		}
	}
	return max(0, up) + max(0, s.Impulse[1])
}

// Allowed returns the bound a move is held to: the allowance passed, with a
// tolerance as a part of it and an epsilon in blocks.
func Allowed(allowance, tolerance, epsilon float64) float64 {
	return allowance*(1+tolerance) + epsilon
}

// HoverLimit returns how many moves in a row a player may float, Java's
// ceil(ticks × max(0.08 / g, 1)): eight times as many under Slow Falling.
func HoverLimit(ticks int, slowFalling bool) int {
	g := Gravity
	if slowFalling {
		g = SlowFallingGravity
	}
	return int(math.Ceil(float64(ticks) * max(Gravity/g, 1)))
}
