package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestTripwireConnectionsTrackNeighboursAndPreserveState(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	wirePos := cube.Pos{0, 64, 0}
	eastPos := wirePos.Side(cube.FaceEast)
	wire := Tripwire{Attached: true, Disarmed: true, Powered: true, Suspended: true}

	runWorld(w, func(tx *world.Tx) {
		tx.SetBlock(wirePos, wire, nil)
		tx.SetBlock(eastPos, Tripwire{}, nil)
		updateTripwireConnections(wirePos, tx)
		got := tx.Block(wirePos).(Tripwire)
		if !got.EastConnection || got.NorthConnection || got.SouthConnection || got.WestConnection {
			t.Fatalf("wire connections with adjacent wire = %#v", got)
		}
		if !got.Attached || !got.Disarmed || !got.Powered || !got.Suspended {
			t.Fatalf("connection update lost other state: %#v", got)
		}

		tx.SetBlock(eastPos, TripwireHook{Facing: cube.West}, nil)
		updateTripwireConnections(wirePos, tx)
		got = tx.Block(wirePos).(Tripwire)
		if !got.EastConnection {
			t.Fatalf("wire did not connect to facing hook: %#v", got)
		}

		tx.SetBlock(eastPos, TripwireHook{Facing: cube.East}, nil)
		updateTripwireConnections(wirePos, tx)
		got = tx.Block(wirePos).(Tripwire)
		if got.EastConnection {
			t.Fatalf("wire connected to hook facing away: %#v", got)
		}

		tx.SetBlock(eastPos, nil, nil)
		updateTripwireConnections(wirePos, tx)
		got = tx.Block(wirePos).(Tripwire)
		if got.EastConnection {
			t.Fatalf("wire retained connection after neighbour removal: %#v", got)
		}
	})
}

// tripwireLine sets up hooks at x 0 and 3 facing each other, with string at
// x 1 and 2 between them, and returns the positions of the hooks and string.
func tripwireLine(tx *world.Tx) (hooks, wires [2]cube.Pos) {
	hooks = [2]cube.Pos{{0, 64, 0}, {3, 64, 0}}
	wires = [2]cube.Pos{{1, 64, 0}, {2, 64, 0}}
	tx.SetBlock(hooks[0].Side(cube.FaceWest), Stone{}, nil)
	tx.SetBlock(hooks[1].Side(cube.FaceEast), Stone{}, nil)
	tx.SetBlock(hooks[0], TripwireHook{Facing: cube.East}, nil)
	tx.SetBlock(hooks[1], TripwireHook{Facing: cube.West}, nil)
	for _, pos := range wires {
		tx.SetBlock(pos, Tripwire{Suspended: true}, nil)
	}
	calculateTripwireHook(hooks[0], tx.Block(hooks[0]).(TripwireHook), false, false, -1, nil, tx)
	return hooks, wires
}

// TestTripwireLineAttachesAndPowers checks TripWireHookBlock.calculateState:
// hooks facing each other over whole string attach it, and string pressed
// powers both hooks.
func TestTripwireLineAttachesAndPowers(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	runWorld(w, func(tx *world.Tx) {
		hooks, wires := tripwireLine(tx)
		for _, pos := range hooks {
			if hook := tx.Block(pos).(TripwireHook); !hook.Attached || hook.Powered {
				t.Fatalf("hook at %v = %#v, want attached and unpowered", pos, hook)
			}
		}
		for _, pos := range wires {
			if wire := tx.Block(pos).(Tripwire); !wire.Attached || !wire.Suspended {
				t.Fatalf("string at %v = %#v, want attached", pos, wire)
			}
		}
		tx.Block(wires[0]).(Tripwire).checkPressed(wires[0], tx, true)
		for _, pos := range hooks {
			if hook := tx.Block(pos).(TripwireHook); !hook.Powered {
				t.Fatalf("pressed string left hook at %v unpowered", pos)
			}
		}
	})
}

// TestBrokenTripwireTripsItsHooks checks TripWireBlock.affectNeighborsAfter
// Removal: string broken by hand powers its hooks as if pressed, until they
// recheck their line ten ticks later and detach, while string cut with shears
// is disarmed and detaches them at once.
func TestBrokenTripwireTripsItsHooks(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	runWorld(w, func(tx *world.Tx) {
		hooks, wires := tripwireLine(tx)
		wire := tx.Block(wires[0]).(Tripwire)
		tx.SetBlock(wires[0], nil, nil)
		wire.BreakInfo().BreakHandler(wires[0], tx, &chiseledBookshelfUser{})
		for _, pos := range hooks {
			if hook := tx.Block(pos).(TripwireHook); !hook.Powered || !hook.Attached {
				t.Fatalf("breaking string left hook at %v = %#v, want it tripped", pos, hook)
			}
		}
		hook := tx.Block(hooks[0]).(TripwireHook)
		hook.ScheduledTick(hooks[0], tx, nil)
		for _, pos := range hooks {
			if hook := tx.Block(pos).(TripwireHook); hook.Powered || hook.Attached {
				t.Fatalf("hook at %v = %#v after rechecking the broken line, want detached", pos, hook)
			}
		}
	})
	runWorld(w, func(tx *world.Tx) {
		hooks, wires := tripwireLine(tx)
		wire := tx.Block(wires[0]).(Tripwire)
		tx.SetBlock(wires[0], nil, nil)
		wire.BreakInfo().BreakHandler(wires[0], tx, &chiseledBookshelfUser{held: item.NewStack(item.Shears{}, 1)})
		for _, pos := range hooks {
			if hook := tx.Block(pos).(TripwireHook); hook.Powered || hook.Attached {
				t.Fatalf("cutting string with shears left hook at %v = %#v, want detached and unpowered", pos, hook)
			}
		}
	})
}

// TestStringPlacesSuspendedTripwire checks string places tripwire like BDS:
// suspended whether or not a block is below it.
func TestStringPlacesSuspendedTripwire(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		target := cube.Pos{0, 64, 0}
		tx.SetBlock(target, Stone{}, nil)
		user := &pistonTestUser{pos: mgl64.Vec3{3, 66, 0.5}}
		for _, face := range []cube.Face{cube.FaceUp, cube.FaceEast} {
			(item.String{}).UseOnBlock(target, face, mgl64.Vec3{}, tx, user, &item.UseContext{})
			wire, ok := tx.Block(target.Side(face)).(Tripwire)
			if !ok || !wire.Suspended {
				t.Fatalf("string on the %v face placed %#v, want suspended tripwire", face, tx.Block(target.Side(face)))
			}
		}
	})
}
