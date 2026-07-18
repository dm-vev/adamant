package entity

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestBoatGroundFrictionAveragesFootprint(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	handle := NewBoat(world.EntitySpawnOpts{Position: mgl64.Vec3{1, 1, 1}}, 0)
	<-w.Exec(func(tx *world.Tx) {
		tx.AddEntity(handle)
		for x := 0; x < 2; x++ {
			for z := 0; z < 2; z++ {
				var surface world.Block = block.Cobblestone{}
				if x == 0 {
					surface = block.Ice{}
				}
				tx.SetBlock(cube.Pos{x, 0, z}, surface, nil)
			}
		}
		boatEntity, _ := handle.Entity(tx)
		boat := boatEntity.(*Boat)
		if got, want := boat.base().groundFriction(tx, boat.Ent), 0.79; math.Abs(got-want) > 1e-9 {
			t.Fatalf("ground friction = %v, want %v", got, want)
		}
	})
}

func TestBoatGroundFrictionInWater(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	handle := NewBoat(world.EntitySpawnOpts{}, 0)
	<-w.Exec(func(tx *world.Tx) {
		tx.AddEntity(handle)
		boatEntity, _ := handle.Entity(tx)
		boat := boatEntity.(*Boat)
		boat.base().inWater = true
		if got, want := boat.base().groundFriction(tx, boat.Ent), 0.9; got != want {
			t.Fatalf("water friction = %v, want %v", got, want)
		}
	})
}

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

func TestBoatPassengerSeatHeight(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	pos := mgl64.Vec3{0, 4, 0}
	boatHandle := NewBoat(world.EntitySpawnOpts{Position: pos}, 0)
	passengerHandle := world.EntitySpawnOpts{Position: pos}.New(seatTestRiderType{}, seatTestRiderConfig{})
	<-w.Exec(func(tx *world.Tx) {
		tx.AddEntity(boatHandle)
		tx.AddEntity(passengerHandle)
		boatEntity, _ := boatHandle.Entity(tx)
		passenger, _ := passengerHandle.Entity(tx)
		rider := passenger.(*seatTestRider)
		boat := boatEntity.(*Boat)
		behaviour := boat.base()
		behaviour.passengers[0] = passengerHandle
		behaviour.updatePassengers(boat.Ent, tx)

		offset := passenger.Position()[1] - boat.Position()[1]
		if got, want := behaviour.SeatOffset()[1], float32(1.02001); got != want {
			t.Fatalf("seat metadata offset = %v, want %v", got, want)
		} else if got != float32(offset) {
			t.Fatalf("seat metadata offset = %v, passenger offset = %v", got, offset)
		}
		rim := boat.Position()[1] + BoatType.BBox(boat).Max()[1]
		eyeY := passenger.Position()[1] + rider.EyeHeight()
		if passenger.Position()[1] <= rim || eyeY <= rim {
			t.Fatalf("passenger base/eye = %v/%v, want above boat rim %v", passenger.Position()[1], eyeY, rim)
		}
	})
}

type seatTestRiderConfig struct{}

func (seatTestRiderConfig) Apply(*world.EntityData) {}

type seatTestRiderType struct{}

func (seatTestRiderType) Open(_ *world.Tx, h *world.EntityHandle, data *world.EntityData) world.Entity {
	return &seatTestRider{h: h, data: data}
}
func (seatTestRiderType) EncodeEntity() string                        { return "test:rider" }
func (seatTestRiderType) BBox(world.Entity) cube.BBox                 { return cube.BBox{} }
func (seatTestRiderType) DecodeNBT(map[string]any, *world.EntityData) {}
func (seatTestRiderType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

type seatTestRider struct {
	h    *world.EntityHandle
	data *world.EntityData
}

func (r *seatTestRider) H() *world.EntityHandle            { return r.h }
func (r *seatTestRider) Position() mgl64.Vec3              { return r.data.Pos }
func (r *seatTestRider) Rotation() cube.Rotation           { return r.data.Rot }
func (r *seatTestRider) EyeHeight() float64                { return 1.62 }
func (r *seatTestRider) Close() error                      { return nil }
func (r *seatTestRider) Move(pos mgl64.Vec3, _, _ float64) { r.data.Pos = r.data.Pos.Add(pos) }

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
