package entity

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
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

func TestBoatNBTRoundTrip(t *testing.T) {
	data := &world.EntityData{Data: BoatBehaviourConfig{Variant: 8}.New()}
	nbt := BoatType.EncodeNBT(data)
	decoded := new(world.EntityData)
	BoatType.DecodeNBT(nbt, decoded)
	if got := decoded.Data.(*BoatBehaviour).variant; nbt["Variant"] != int32(8) || got != 8 {
		t.Fatalf("boat variant round trip: nbt=%v variant=%d", nbt, got)
	}
}

func TestChestBoatNBTRoundTrip(t *testing.T) {
	behaviour := chestBoatConf.New()
	behaviour.variant = 9
	_ = behaviour.inv.SetItem(26, item.NewStack(item.Diamond{}, 3))
	nbt := ChestBoatType.EncodeNBT(&world.EntityData{Data: behaviour})
	decoded := new(world.EntityData)
	ChestBoatType.DecodeNBT(nbt, decoded)
	got := decoded.Data.(*ChestBoatBehaviour)
	stack, _ := got.inv.Item(26)
	if got.variant != 9 || stack.Count() != 3 {
		t.Fatalf("chest boat round trip: variant=%d stack=%v nbt=%v", got.variant, stack, nbt)
	}
}
