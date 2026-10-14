package entity

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// seat is one seat of a vehicle's Bedrock minecraft:rideable component.
type seat struct {
	// pos is the seat's position relative to the vehicle.
	pos mgl64.Vec3
	// lock is lock_rider_rotation in degrees, if hasLock is set.
	lock    float64
	hasLock bool
	// rotate is rotate_rider_by in degrees, if hasRotate is set.
	rotate    float64
	hasRotate bool
}

// playerSeatHeight is how much higher Bedrock seats a player than its seat
// position: player positions are eye positions.
const playerSeatHeight = 1.22001

// riderSeat returns the seat rider takes in s.
func (s seat) riderSeat(rider world.Entity) world.RiderSeat {
	out := world.RiderSeat{Position: s.pos, LockRotation: s.hasLock, LockRotationDegrees: 181, RotateRider: s.hasRotate, RotateRiderDegrees: s.rotate}
	if s.hasLock {
		out.LockRotationDegrees = s.lock
	}
	if rider.H().Type().EncodeEntity() == "minecraft:player" {
		out.Position[1] += playerSeatHeight
	}
	return out
}

// RiderSeater is implemented by vehicles that seat riders.
type RiderSeater interface {
	// RiderSeat returns the seat rider, a passenger of the vehicle, takes.
	RiderSeat(rider world.Entity) world.RiderSeat
}

// seatHolder is implemented by riders that keep the seat they ride in.
type seatHolder interface {
	SetRiderSeat(world.RiderSeat)
}

// seatRider makes rider ride vehicle in the seat the vehicle gives it.
func seatRider(rider Rider, vehicle world.Entity) {
	rider.SetRiding(vehicle.H())
	reseatRider(rider, vehicle)
}

// reseatRider gives rider, a passenger of vehicle, the seat vehicle now
// gives it. Vehicles whose seats depend on how many ride call it for every
// passenger when a passenger mounts or dismounts.
func reseatRider(rider any, vehicle world.Entity) {
	holder, ok := rider.(seatHolder)
	e, isEntity := rider.(world.Entity)
	if !ok || !isEntity {
		return
	}
	seater, isSeater := vehicle.(RiderSeater)
	if !isSeater {
		if b, hasBehaviour := vehicle.(interface{ Behaviour() Behaviour }); hasBehaviour {
			seater, isSeater = b.Behaviour().(RiderSeater)
		}
	}
	if isSeater {
		holder.SetRiderSeat(seater.RiderSeat(e))
	}
}
