package entity

import (
	"context"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestPortalTravelWaitsWhileTransfersAreHeld checks that an entity entering
// a portal while a checkpoint holds transfers out of its world stays where
// it is, ready to travel, and travels at its next contact with the portal
// once the hold is lifted.
func TestPortalTravelWaitsWhileTransfersAreHeld(t *testing.T) {
	t.Parallel()
	var overworld, nether *world.World
	overworld = world.Config{PortalDestination: func(dim world.Dimension) *world.World {
		if dim == world.Nether {
			return nether
		}
		return nil
	}}.New()
	nether = world.Config{Dim: world.Nether, PortalDestination: func(dim world.Dimension) *world.World {
		if dim == world.Nether {
			return overworld
		}
		return nil
	}}.New()
	t.Cleanup(func() {
		_ = overworld.Close()
		_ = nether.Close()
	})
	sourcePos, targetPortal := mgl64.Vec3{80.5, 64, 80.5}, cube.Pos{10, 64, 10}
	mustDo(t, nether, func(tx *world.Tx) { buildActivePortal(tx, targetPortal) })

	release, err := world.HoldTransfers(context.Background(), []*world.World{overworld})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	handle := world.EntitySpawnOpts{Position: sourcePos}.New(EnderPearlType, enderPearlConf)
	enter := func(tx *world.Tx) {
		e, ok := handle.Entity(tx)
		if !ok {
			t.Fatal("the pearl is not in the Overworld")
		}
		(block.Portal{Axis: cube.Z}).EntityInside(cube.PosFromVec3(sourcePos), tx, e)
	}
	mustDo(t, overworld, func(tx *world.Tx) {
		tx.AddEntity(handle)
		enter(tx)
	})
	time.Sleep(100 * time.Millisecond)
	if !entityInWorld(handle, overworld) {
		t.Fatal("the pearl left the Overworld while transfers out of it were held")
	}
	mustDo(t, overworld, func(tx *world.Tx) {
		e, _ := handle.Entity(tx)
		tc := e.(*Ent).Behaviour().(*ProjectileBehaviour).PortalTravelComputer()
		tc.mu.Lock()
		defer tc.mu.Unlock()
		if tc.travelling || tc.timedOut || !tc.awaitingTravel {
			t.Errorf("the travel held: travelling=%t timedOut=%t awaitingTravel=%t, want ready to travel at the next contact", tc.travelling, tc.timedOut, tc.awaitingTravel)
		}
	})
	release()
	mustDo(t, overworld, enter)
	waitForEntityWorld(t, handle, nether)
}
