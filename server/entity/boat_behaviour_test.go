package entity

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestControlledBoatTurnKeepsInputYaw(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	boatHandle := NewBoat(world.EntitySpawnOpts{Velocity: mgl64.Vec3{0.2, 0, 0}}, 0)
	passengerHandle := NewCow(world.EntitySpawnOpts{})
	<-w.Exec(func(tx *world.Tx) {
		tx.AddEntity(boatHandle)
		tx.AddEntity(passengerHandle)
		boatEntity, _ := boatHandle.Entity(tx)
		boat := boatEntity.(*Boat)
		behaviour := boat.base()
		behaviour.passengers[0] = passengerHandle
		behaviour.SetVehicleInput(1, 0)

		movement := behaviour.Tick(boat.Ent, tx)
		defer movement.Send()
		if got, want := boat.Rotation(), (cube.Rotation{-0.6, 0}); math.Abs(got.Yaw()-want.Yaw()) > 1e-6 || got.Pitch() != want.Pitch() {
			t.Fatalf("controlled boat rotation = %v, want %v", got, want)
		}
	})
}
