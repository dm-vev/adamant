package world

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

// MoveParts are the parts of an EntityMove that changed since the viewers
// of the entity were last shown a move of it.
type MoveParts uint8

const (
	MoveX MoveParts = 1 << iota
	MoveY
	MoveZ
	MovePitch
	MoveYaw
	MoveHeadYaw
	// MoveGround marks an entity that left or reached the ground.
	MoveGround
	// MoveVelocity marks a velocity the viewers are shown with the move, or
	// alone if no other part changed.
	MoveVelocity

	// MovePosition and MoveRotation are all the parts of a position and of
	// a rotation, MoveAll all those of both.
	MovePosition = MoveX | MoveY | MoveZ
	MoveRotation = MovePitch | MoveYaw | MoveHeadYaw
	MoveAll      = MovePosition | MoveRotation
)

// EntityMove is a move of an entity that every viewer of it is shown alike
// on a tick: where the entity is, which parts of that changed since the
// viewers were last shown a move of it, and over how many ticks to show
// it. The viewers are shown it one after the other on the goroutine of the
// world.
type EntityMove struct {
	Pos      mgl64.Vec3
	Rot      cube.Rotation
	HeadYaw  float64
	Vel      mgl64.Vec3
	OnGround bool
	// Changed are the parts that changed.
	Changed MoveParts
	// Ticks is the number of ticks a viewer moves the entity over; zero
	// moves it at once.
	Ticks int
	// Absolute shows the move as the position of the entity rather than as
	// the parts that changed: Changed and Ticks are left out.
	Absolute bool
	// Shared is left to the viewers: the first may keep there what it made
	// of the move for the others to use.
	Shared any
}

// The movement of an entity goes to its viewers as BDS sends it: that of a
// mob every MoveInterval ticks as a move of the parts that changed, which
// clients carry out over those ticks, that of an item or a projectile on
// every tick it changes, at once, and either at once when the entity
// leaves or reaches the ground. Its velocity goes with a move, or alone,
// when it changed by more than moveVelocityChange since it was last shown.
// Every viewer is shown the same move, so that it is encoded once for all.
const (
	// MoveInterval is the number of ticks between the moves of a mob shown
	// to its viewers, as Java sends those of most mobs.
	MoveInterval = 3
	// moveTeleport is how far an entity goes between two moves for its
	// viewers to be shown a teleport rather than a move.
	moveTeleport = 8.0
	// movePrecision is the least change of a coordinate shown, as Java
	// encodes positions in 1/4096 blocks.
	movePrecision = 1.0 / 4096
	// moveVelocityChange is the squared change of the velocity of an entity
	// since it was last shown over which BDS shows it again: 0.2 blocks a
	// tick.
	moveVelocityChange = 0.04
)

// MoveTracker holds what the viewers of an entity were last shown of its
// movement. Its zero value has shown nothing.
type MoveTracker struct {
	shown            bool
	pos              mgl64.Vec3
	pitch, yaw, head uint8
	onGround         bool
	// vel is the velocity last shown, zero before any, as BDS has it.
	vel mgl64.Vec3
	// since counts the ticks since the last move shown.
	since int
	// moving notes an entity that moved on the last tick.
	moving bool
}

// Move returns the move the viewers of an entity are to be shown on this
// tick, given where the entity is, whether it moved on the tick and the
// ticks between its moves, which clients carry them out over: MoveInterval
// for a mob, zero for an entity shown at once on every tick it changes. It
// returns nil if no move is due, or teleport for a movement too far for a
// move, which the viewers are to be shown as it is.
func (t *MoveTracker) Move(pos mgl64.Vec3, rot cube.Rotation, headYaw float64, vel mgl64.Vec3, onGround, moving bool, interval int) (move *EntityMove, teleport bool) {
	t.since++
	stopped := t.moving && !moving
	t.moving = moving
	pitch, yaw, head := angleByte(rot.Pitch()), angleByte(rot.Yaw()), angleByte(headYaw)
	first := !t.shown
	if first && !moving {
		// The viewers have the entity where they were shown it.
		t.remember(pos, pitch, yaw, head, onGround)
		return nil, false
	}
	if !first && pos.Sub(t.pos).Len() > moveTeleport {
		t.remember(pos, pitch, yaw, head, onGround)
		return nil, true
	}
	var changed MoveParts
	for i, part := range [...]MoveParts{MoveX, MoveY, MoveZ} {
		if math.Abs(pos[i]-t.pos[i]) >= movePrecision {
			changed |= part
		}
	}
	if pitch != t.pitch {
		changed |= MovePitch
	}
	if yaw != t.yaw {
		changed |= MoveYaw
	}
	if head != t.head {
		changed |= MoveHeadYaw
	}
	if onGround != t.onGround {
		changed |= MoveGround
	}
	if first {
		// What the viewers were shown of the entity is not known here: the
		// first move shows all of it.
		changed = MoveAll
	}
	ticks := 0
	switch {
	case changed == 0:
	case first || changed&MoveGround != 0:
		// The first move, and leaving or reaching the ground, are shown at
		// once.
	case t.since >= interval || stopped:
		// The entity is shown where it got to over the ticks since the
		// last move; one that stopped is shown where it stopped.
		ticks = min(t.since, interval)
	default:
		changed = 0
	}
	if changed != 0 {
		t.remember(pos, pitch, yaw, head, onGround)
	}
	if vel.Sub(t.vel).LenSqr() > moveVelocityChange {
		changed |= MoveVelocity
		t.vel = vel
	}
	if changed == 0 {
		return nil, false
	}
	return &EntityMove{Pos: pos, Rot: rot, HeadYaw: headYaw, Vel: vel, OnGround: onGround, Changed: changed, Ticks: ticks}, false
}

// Shown notes a position the viewers of the entity were shown other than
// by the tracker, as by a teleport, for the moves after it to start from.
func (t *MoveTracker) Shown(pos mgl64.Vec3, rot cube.Rotation, headYaw float64, onGround bool) {
	t.remember(pos, angleByte(rot.Pitch()), angleByte(rot.Yaw()), angleByte(headYaw), onGround)
}

// remember notes a position as shown.
func (t *MoveTracker) remember(pos mgl64.Vec3, pitch, yaw, head uint8, onGround bool) {
	t.shown, t.since = true, 0
	t.pos, t.pitch, t.yaw, t.head, t.onGround = pos, pitch, yaw, head, onGround
}

// angleByte returns an angle as the protocol sends it, in 256ths of a turn.
func angleByte(deg float64) uint8 {
	return uint8(int64(float32(deg) / (360.0 / 256.0)))
}
