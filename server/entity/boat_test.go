package entity

import (
	"math"
	"testing"
	_ "unsafe"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func init() {
	worldFinaliseBlockRegistry()
}

//go:linkname worldFinaliseBlockRegistry github.com/df-mc/dragonfly/server/world.finaliseBlockRegistry
func worldFinaliseBlockRegistry()

func TestBoatForwardMotionFollowsYaw(t *testing.T) {
	w := world.Config{Entities: DefaultRegistry}.New()
	defer w.Close()

	var (
		pos     mgl64.Vec3
		yaw     float64
		execErr error
	)
	<-w.Exec(func(tx *world.Tx) {
		base := cube.Pos{0, 63, 0}
		prepareBoatArena(tx, base)

		handle := world.EntitySpawnOpts{Position: base.Vec3Centre().Add(mgl64.Vec3{0, 0.1, 0})}.New(BoatType, BoatBehaviourConfig{})
		ent, ok := tx.AddEntity(handle).(*Ent)
		if !ok {
			execErr = errTypeAssertion("*entity.Ent")
			return
		}
		boat, ok := ent.Behaviour().(*BoatBehaviour)
		if !ok {
			execErr = errTypeAssertion("*entity.BoatBehaviour")
			return
		}

		for i := 0; i < 40; i++ {
			boat.SetInput(1, false, false, 90, true)
			if m := boat.Tick(ent, tx); m != nil {
				m.Send()
			}
		}

		pos = ent.Position()
		yaw = ent.Rotation().Yaw()
	})

	if execErr != nil {
		t.Fatalf("forward simulation failed: %v", execErr)
	}

	if yaw < 80 || yaw > 100 {
		t.Fatalf("unexpected yaw after forward motion: got %.2f", yaw)
	}
	if pos[0] >= -0.5 {
		t.Fatalf("boat did not move west as expected: pos=%v", pos)
	}
	if math.Abs(pos[2]) > math.Abs(pos[0])*0.2+0.05 {
		t.Fatalf("boat drifted sideways disproportionately: pos=%v", pos)
	}
}

func TestBoatRemainsAfloatWithPassengerWeight(t *testing.T) {
	boat := BoatBehaviourConfig{}.New()
	water := block.Water{Depth: 8, Still: true}

	lift := boat.applyLiquidBuoyancy(-0.1, 62.7, 62, water, 1)
	if lift <= -0.1 {
		t.Fatalf("expected upward acceleration without passengers, got %f", lift)
	}

	boat.passengerCount.Store(2)
	heavyLift := boat.applyLiquidBuoyancy(-0.1, 62.7, 62, water, 1)
	if heavyLift <= -0.1 {
		t.Fatalf("expected upward acceleration with passengers, got %f", heavyLift)
	}
	if heavyLift >= lift {
		t.Fatalf("expected heavier boat to have reduced lift: heavy=%f base=%f", heavyLift, lift)
	}
}

func TestBoatInputClearedOnPassengerRemoval(t *testing.T) {
	boat := BoatBehaviourConfig{}.New()
	boat.SetInput(1, true, true, 45, true)

	handle := &world.EntityHandle{}
	if _, ok := boat.AddPassenger(handle); !ok {
		t.Fatalf("failed to add passenger")
	}

	boat.RemovePassenger(handle)

	if boat.input.forward != 0 {
		t.Fatalf("forward input not cleared after passenger removal")
	}
	if boat.haveVehicleYaw {
		t.Fatalf("vehicle yaw persisted after passenger removal")
	}
	if boat.leftPaddle != 0 || boat.rightPaddle != 0 {
		t.Fatalf("paddle state not cleared after passenger removal")
	}
	if len(boat.Passengers()) != 0 {
		t.Fatalf("passengers slice not cleared after removal")
	}
}

func prepareBoatArena(tx *world.Tx, centre cube.Pos) {
	baseY := centre[1]
	for x := -1; x <= 1; x++ {
		for z := -1; z <= 1; z++ {
			floor := cube.Pos{centre[0] + x, baseY - 1, centre[2] + z}
			tx.SetBlock(floor, block.Stone{}, nil)
			tx.SetBlock(floor.Side(cube.FaceUp), block.Water{Depth: 8, Still: true}, nil)
		}
	}
}

type errTypeAssertion string

func (e errTypeAssertion) Error() string {
	return "unexpected entity type assertion failure: " + string(e)
}
