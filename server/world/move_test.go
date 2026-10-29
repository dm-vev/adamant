package world

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

// TestMoveTrackerAsBDS walks a mob as BDS shows one: a move every
// MoveInterval ticks of the parts that changed, carried out over those
// ticks, a move at once when it leaves or reaches the ground, its velocity
// whenever that changed by over 0.2 blocks a tick since it was last shown,
// and a teleport when it goes too far.
func TestMoveTrackerAsBDS(t *testing.T) {
	var tr MoveTracker
	rot := cube.Rotation{90, 0}
	pos := mgl64.Vec3{0.5, 64, 0.5}
	if move, teleport := tr.Move(pos, rot, 90, mgl64.Vec3{}, true, false, MoveInterval); move != nil || teleport {
		t.Fatal("a mob standing where its viewers were shown it was moved")
	}
	// Walking along x: nothing until the interval, then x alone, and no
	// velocity, which changes by less than 0.2.
	var moves []*EntityMove
	for tick := range 9 {
		pos[0] += 0.1
		move, teleport := tr.Move(pos, rot, 90, mgl64.Vec3{0.1, 0, 0}, true, true, MoveInterval)
		if teleport {
			t.Fatalf("tick %d: teleport", tick)
		}
		if move != nil {
			moves = append(moves, move)
		}
	}
	if len(moves) != 3 {
		t.Fatalf("%d moves in 9 ticks, want 3", len(moves))
	}
	for _, m := range moves {
		if m.Changed != MoveX || m.Ticks != MoveInterval || !m.OnGround {
			t.Fatalf("move %+v, want x over %d ticks", m, MoveInterval)
		}
	}
	// A turn of the head alone is shown with the next move.
	pos[0] += 0.1
	_, _ = tr.Move(pos, rot, 135, mgl64.Vec3{}, true, true, MoveInterval)
	pos[0] += 0.1
	_, _ = tr.Move(pos, rot, 135, mgl64.Vec3{}, true, true, MoveInterval)
	pos[0] += 0.1
	move, _ := tr.Move(pos, rot, 135, mgl64.Vec3{}, true, true, MoveInterval)
	if move == nil || move.Changed != MoveX|MoveHeadYaw {
		t.Fatalf("move %+v, want x and the head", move)
	}
	// Stopping shows where the entity stopped, at once.
	pos[0] += 0.1
	if move, _ := tr.Move(pos, rot, 135, mgl64.Vec3{}, true, true, MoveInterval); move != nil {
		t.Fatal("a move before the interval")
	}
	if move, _ := tr.Move(pos, rot, 135, mgl64.Vec3{}, true, false, MoveInterval); move == nil || move.Changed != MoveX || move.Ticks != 2 {
		t.Fatalf("stopping: %+v, want x over the 2 ticks since the last move", move)
	}
	if move, _ := tr.Move(pos, rot, 135, mgl64.Vec3{}, true, false, MoveInterval); move != nil {
		t.Fatal("a move of an entity standing still")
	}
	// Jumping: at once, with the velocity, then moves alone in the air.
	pos[1] += 0.42
	move, _ = tr.Move(pos, rot, 135, mgl64.Vec3{0, 0.42, 0}, false, true, MoveInterval)
	if move == nil || move.Ticks != 0 || move.OnGround || move.Changed != MoveY|MoveGround|MoveVelocity || move.Vel != (mgl64.Vec3{0, 0.42, 0}) {
		t.Fatalf("jump: %+v", move)
	}
	var airborne *EntityMove
	for range MoveInterval {
		pos[1] += 0.3
		if m, _ := tr.Move(pos, rot, 135, mgl64.Vec3{0, 0.3, 0}, false, true, MoveInterval); m != nil {
			airborne = m
		}
	}
	if airborne == nil || airborne.Changed != MoveY || airborne.Ticks != MoveInterval {
		t.Fatalf("in the air: %+v", airborne)
	}
	// A push in the air changes the velocity by more than 0.2: it is shown
	// at once, without the move, which is not due.
	pos[1] += 0.3
	move, _ = tr.Move(pos, rot, 135, mgl64.Vec3{0.5, 0.3, 0}, false, true, MoveInterval)
	if move == nil || move.Changed != MoveVelocity || move.Vel != (mgl64.Vec3{0.5, 0.3, 0}) {
		t.Fatalf("push: %+v, want the velocity alone", move)
	}
	pos[1] -= 1.62
	move, _ = tr.Move(pos, rot, 135, mgl64.Vec3{}, true, true, MoveInterval)
	if move == nil || move.Ticks != 0 || !move.OnGround || move.Changed != MoveY|MoveGround|MoveVelocity {
		t.Fatalf("landing: %+v", move)
	}
	// Too far for a move.
	pos[2] += 9
	if move, teleport := tr.Move(pos, rot, 135, mgl64.Vec3{}, true, true, MoveInterval); move != nil || !teleport {
		t.Fatal("a move of 9 blocks is not a teleport")
	}
}

// TestMoveTrackerEveryTick drops an item as BDS shows one: a move on every
// tick it moves, at once, its velocity once it changed by more than 0.2
// blocks a tick from the zero BDS starts from, and again when it lands.
func TestMoveTrackerEveryTick(t *testing.T) {
	var tr MoveTracker
	pos := mgl64.Vec3{0.5, 70, 0.5}
	_, _ = tr.Move(pos, cube.Rotation{}, 0, mgl64.Vec3{}, false, false, 0)
	var vel mgl64.Vec3
	for tick := 1; tick <= 8; tick++ {
		vel[1] -= 0.045
		pos[1] += vel[1]
		move, _ := tr.Move(pos, cube.Rotation{}, 0, vel, false, true, 0)
		if move == nil || move.Changed&^MoveVelocity != MoveY || move.Ticks != 0 {
			t.Fatalf("tick %d: %+v, want y at once", tick, move)
		}
		if shown := move.Changed&MoveVelocity != 0; shown != (tick == 5) {
			t.Fatalf("tick %d: velocity %v shown: %v", tick, vel, shown)
		}
	}
	pos[1] = 64
	move, _ := tr.Move(pos, cube.Rotation{}, 0, mgl64.Vec3{}, true, true, 0)
	if move == nil || move.Changed != MoveY|MoveGround|MoveVelocity || move.Vel != (mgl64.Vec3{}) || move.Ticks != 0 {
		t.Fatalf("landing: %+v", move)
	}
	// Lying still, with the gravity of a few ticks in its velocity until it
	// next moves, as an item does.
	for tick := range 8 {
		if move, _ := tr.Move(pos, cube.Rotation{}, 0, mgl64.Vec3{0, -0.04 * float64(tick%4), 0}, true, false, 0); move != nil {
			t.Fatalf("a move of an item lying still: %+v", move)
		}
	}
}

// TestMoveTrackerAngles shows a turn only once it changes the angle the
// protocol sends, in 256ths of a turn.
func TestMoveTrackerAngles(t *testing.T) {
	var tr MoveTracker
	pos := mgl64.Vec3{}
	_, _ = tr.Move(pos, cube.Rotation{0, 0}, 0, mgl64.Vec3{}, true, false, MoveInterval)
	for range MoveInterval {
		if move, _ := tr.Move(pos, cube.Rotation{1, 0}, 1, mgl64.Vec3{}, true, true, MoveInterval); move != nil {
			t.Fatalf("a turn of a degree, under a 256th of a turn: %+v", move)
		}
	}
	for range MoveInterval {
		if move, _ := tr.Move(pos, cube.Rotation{2, -10}, 2, mgl64.Vec3{}, true, true, MoveInterval); move != nil {
			if move.Changed != MoveRotation {
				t.Fatalf("turn: %+v", move)
			}
			return
		}
	}
	t.Fatal("a turn of two degrees was not shown")
}

// TestMoveTrackerShown starts the moves after a teleport from where the
// teleport put the entity, for parts to be left out only where the client
// has them.
func TestMoveTrackerShown(t *testing.T) {
	var tr MoveTracker
	_, _ = tr.Move(mgl64.Vec3{10, 64, 10}, cube.Rotation{}, 0, mgl64.Vec3{}, true, false, 0)
	tr.Shown(mgl64.Vec3{12, 64, 10}, cube.Rotation{}, 0, true)
	move, _ := tr.Move(mgl64.Vec3{10, 64, 11}, cube.Rotation{}, 0, mgl64.Vec3{}, true, true, 0)
	if move == nil || move.Changed != MoveX|MoveZ {
		t.Fatalf("move %+v after a teleport, want x back and z", move)
	}
}

// TestMoveTrackerFirstMove shows all of the first move of an entity that
// moved since its viewers were shown it, at once, as the tracker does not
// know where they have it.
func TestMoveTrackerFirstMove(t *testing.T) {
	var tr MoveTracker
	move, teleport := tr.Move(mgl64.Vec3{1, 70, 1}, cube.Rotation{}, 0, mgl64.Vec3{0, -0.1, 0}, false, true, MoveInterval)
	if teleport || move == nil || move.Changed != MoveAll || move.Ticks != 0 {
		t.Fatalf("first move %+v, want all of it at once", move)
	}
}
