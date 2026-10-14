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

func TestTripwireLineTransitionsPreserveConnections(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	hookPos := cube.Pos{0, 64, 0}
	wirePos := hookPos.Side(cube.FaceEast)
	otherHookPos := wirePos.Side(cube.FaceEast)

	runWorld(w, func(tx *world.Tx) {
		leftHook := TripwireHook{Facing: cube.East}
		tx.SetBlock(hookPos, leftHook, nil)
		tx.SetBlock(wirePos, Tripwire{
			Powered: true, Disarmed: true,
			EastConnection: true, WestConnection: true,
		}, nil)
		tx.SetBlock(otherHookPos, TripwireHook{Facing: cube.West}, nil)
		updateTripwireConnections(wirePos, tx)
		updateTripwireLine(hookPos, leftHook, tx)

		got := tx.Block(wirePos).(Tripwire)
		if !got.Attached || !got.Powered || !got.Disarmed || !got.EastConnection || !got.WestConnection {
			t.Fatalf("attached/powered line transition lost state: %#v", got)
		}
		if !tx.Block(hookPos).(TripwireHook).Powered || !tx.Block(otherHookPos).(TripwireHook).Powered {
			t.Fatal("powered wire did not power both hooks")
		}

		tx.SetBlock(otherHookPos, nil, nil)
		updateTripwireLine(hookPos, tx.Block(hookPos).(TripwireHook), tx)
		got = tx.Block(wirePos).(Tripwire)
		if got.Attached || !got.Powered || !got.Disarmed || got.EastConnection || !got.WestConnection {
			t.Fatalf("detached line transition lost wire state: %#v", got)
		}
	})
}

func TestTripwirePowerTransitionPreservesConnections(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	pos := cube.Pos{0, 64, 0}

	runWorld(w, func(tx *world.Tx) {
		wire := Tripwire{
			Powered: true, Attached: true, Disarmed: true, Suspended: true,
			NorthConnection: true, EastConnection: true, SouthConnection: true, WestConnection: true,
		}
		tx.SetBlock(pos, wire, nil)
		wire.updatePowered(pos, tx)
		got := tx.Block(pos).(Tripwire)
		if got.Powered || !got.Attached || !got.Disarmed || !got.Suspended ||
			!got.NorthConnection || !got.EastConnection || !got.SouthConnection || !got.WestConnection {
			t.Fatalf("power transition lost tripwire state: %#v", got)
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
