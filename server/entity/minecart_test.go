package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestMinecartMovesOnStraightRails(t *testing.T) {
	for _, test := range []struct {
		name string
		dir  block.RailDirection
		vel  mgl64.Vec3
		axis int
	}{
		{"east-west", block.RailEastWest, mgl64.Vec3{0.1, 0, 0}, 0},
		{"north-south", block.RailNorthSouth, mgl64.Vec3{0, 0, 0.1}, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := world.Config{Synchronous: true}.New()
			defer w.Close()

			<-w.Exec(func(tx *world.Tx) {
				pos := cube.Pos{0, 1, 0}
				placeMinecartRailLine(tx, pos, test.dir, block.Rail{Direction: test.dir})
				handle := world.EntitySpawnOpts{Position: pos.Vec3Middle(), Velocity: test.vel}.New(MinecartType, minecartConf)
				tx.AddEntity(handle)
				entity, _ := handle.Entity(tx)
				cart := entity.(*Minecart)
				before := cart.Position()

				for range 8 {
					cart.Behaviour().(*MinecartBehaviour).Tick(cart.Ent, tx).Send()
				}
				if got := cart.Position()[test.axis]; got <= before[test.axis]+0.5 {
					t.Fatalf("position on axis %d = %v, want > %v", test.axis, got, before[test.axis]+0.5)
				}
			})
		})
	}
}

func TestMinecartAcceleratesOnPoweredRail(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{0, 1, 0}
		placeMinecartRailLine(tx, pos, block.RailEastWest, block.PoweredRail{Direction: block.RailEastWest, Powered: true})
		tx.SetBlock(pos.Side(cube.FaceDown), block.RedstoneBlock{}, nil)
		handle := world.EntitySpawnOpts{Position: pos.Vec3Middle(), Velocity: mgl64.Vec3{0.1, 0, 0}}.New(MinecartType, minecartConf)
		tx.AddEntity(handle)
		entity, _ := handle.Entity(tx)
		cart := entity.(*Minecart)

		cart.Behaviour().(*MinecartBehaviour).Tick(cart.Ent, tx).Send()
		if got := cart.Velocity()[0]; got <= 0.1 {
			t.Fatalf("powered velocity = %v, want > 0.1", got)
		}
	})
}

func TestRiderInputStartsMinecart(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{0, 1, 0}
		placeMinecartRailLine(tx, pos, block.RailNorthSouth, block.Rail{Direction: block.RailNorthSouth})
		cartHandle := world.EntitySpawnOpts{Position: pos.Vec3Middle()}.New(MinecartType, minecartConf)
		riderHandle := NewCow(world.EntitySpawnOpts{Position: pos.Vec3Middle()})
		tx.AddEntity(cartHandle)
		tx.AddEntity(riderHandle)
		entity, _ := cartHandle.Entity(tx)
		cart := entity.(*Minecart)
		behaviour := cart.Behaviour().(*MinecartBehaviour)
		behaviour.passenger = riderHandle
		behaviour.SetVehicleInput(0, 1)
		before := cart.Position()[2]

		for range 2 {
			behaviour.Tick(cart.Ent, tx).Send()
		}
		if got := cart.Position()[2]; got <= before+0.1 {
			t.Fatalf("position = %v, want > %v", got, before+0.1)
		}
	})
}

func TestMinecartPassengerSeatHeight(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	pos := mgl64.Vec3{0, 4, 0}
	cartHandle := world.EntitySpawnOpts{Position: pos}.New(MinecartType, minecartConf)
	passengerHandle := world.EntitySpawnOpts{Position: pos}.New(seatTestRiderType{}, seatTestRiderConfig{})
	<-w.Exec(func(tx *world.Tx) {
		tx.AddEntity(cartHandle)
		tx.AddEntity(passengerHandle)
		cartEntity, _ := cartHandle.Entity(tx)
		passenger, _ := passengerHandle.Entity(tx)
		rider := passenger.(*seatTestRider)
		cart := cartEntity.(*Minecart)
		behaviour := cart.Behaviour().(*MinecartBehaviour)
		behaviour.passenger = passengerHandle
		behaviour.updatePassenger(cart.Ent, tx)

		offset := passenger.Position()[1] - cart.Position()[1]
		if got, want := behaviour.SeatOffset()[1], float32(0.75); got != want {
			t.Fatalf("seat metadata offset = %v, want %v", got, want)
		} else if got != float32(offset) {
			t.Fatalf("seat metadata offset = %v, passenger offset = %v", got, offset)
		}
		rim := cart.Position()[1] + MinecartType.BBox(cart).Max()[1]
		eyeY := passenger.Position()[1] + rider.EyeHeight()
		if passenger.Position()[1] <= rim || eyeY <= rim {
			t.Fatalf("passenger base/eye = %v/%v, want above minecart rim %v", passenger.Position()[1], eyeY, rim)
		}
	})
}

func placeMinecartRailLine(tx *world.Tx, pos cube.Pos, direction block.RailDirection, centre world.Block) {
	for offset := -1; offset <= 1; offset++ {
		railPos := pos.Add(cube.Pos{offset, 0, 0})
		if direction == block.RailNorthSouth {
			railPos = pos.Add(cube.Pos{0, 0, offset})
		}
		tx.SetBlock(railPos.Side(cube.FaceDown), block.Cobblestone{}, nil)
		tx.SetBlock(railPos, block.Rail{Direction: direction}, nil)
	}
	tx.SetBlock(pos, centre, nil)
}
