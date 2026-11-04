package player

import (
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
)

// ridden returns the entity the player rides, if any, and whether it rides
// one at all: an entity it rides may be in another world, or gone.
func (p *Player) ridden() (e world.Entity, ok, riding bool) {
	handle := p.Riding()
	if handle == nil {
		return nil, false, false
	}
	e, ok = handle.Entity(p.tx)
	return e, ok, true
}

// SteerVehicle passes the movement input of the player, strafe and forward,
// to the entity it rides, if that entity takes input and the player controls
// it, as does jumping, if the entity takes jumps. It reports whether the
// player rides an entity, in which case the client of the player does not
// move it.
func (p *Player) SteerVehicle(strafe, forward float64, jumping bool) (riding bool) {
	ridden, ok, riding := p.ridden()
	if !ok {
		return riding
	}
	input, ok := ridden.(entity.VehicleInput)
	if !ok {
		return riding
	}
	if controller, ok := ridden.(entity.VehicleController); ok && !controller.ControlsVehicle(p) {
		return riding
	}
	if withRider, ok := ridden.(entity.VehicleInputWithRider); ok {
		withRider.SetVehicleInputFrom(p, strafe, forward)
	} else {
		input.SetVehicleInput(strafe, forward)
	}
	if jump, ok := ridden.(entity.VehicleJumpInput); ok {
		jump.SetVehicleJumping(jumping)
	}
	return riding
}

// DashVehicle makes the entity the player rides dash, as a camel does when
// its rider jumps, and reports whether it did.
func (p *Player) DashVehicle() bool {
	ridden, ok, _ := p.ridden()
	if !ok {
		return false
	}
	dashable, ok := ridden.(interface {
		Dash(*world.Tx, world.Entity) bool
	})
	return ok && dashable.Dash(p.tx, p)
}

// RideJump makes the entity the player rides jump, as a horse does when its
// rider jumps, and reports whether it did.
func (p *Player) RideJump() bool {
	ridden, ok, _ := p.ridden()
	if !ok {
		return false
	}
	jumper, ok := ridden.(interface{ RideJump() bool })
	return ok && jumper.RideJump()
}

// DismountVehicle dismounts the player from the entity it rides, and reports
// whether that entity let it off.
func (p *Player) DismountVehicle() bool {
	ridden, ok, _ := p.ridden()
	if !ok {
		return false
	}
	return p.dismountFrom(ridden)
}

// LeaveVehicle dismounts the player from e, as its client leaves e, if e
// lets passengers off.
func (p *Player) LeaveVehicle(e world.Entity) {
	p.dismountFrom(e)
}

// dismountFrom dismounts the player from e, if e lets passengers off, and
// reports whether it does.
func (p *Player) dismountFrom(e world.Entity) bool {
	dismountable, ok := e.(entity.PassengerDismounter)
	if ok {
		dismountable.Dismount(p.tx, p)
	}
	return ok
}
