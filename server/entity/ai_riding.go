package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports how a mob riding another mob steers it: Java's
// Mob.getControllingPassenger, getNavigation, getMoveControl and
// updateControlFlags.

// firstPassenger returns the first passenger of a vehicle, if it carries one.
// Vehicles that keep no seats of their own are found riding by their
// passengers alone.
func firstPassenger(tx *world.Tx, vehicle world.Entity) (world.Entity, bool) {
	if m := mountFromEntity(vehicle); m != nil {
		if h := m.b().passengers[0]; h != nil {
			return h.Entity(tx)
		}
		return nil, false
	}
	if l, ok := vehicle.(Linker); ok {
		for _, link := range l.Links() {
			if link.Ridden == vehicle.H() && link.Rider != nil {
				return link.Rider.Entity(tx)
			}
		}
		return nil, false
	}
	if passengers := passengersOf(tx, vehicle); len(passengers) > 0 {
		return passengers[0], true
	}
	return nil, false
}

// controllingMob ports Mob.getControllingPassenger for a mob passenger: a
// mob with its AI on is steered by its first passenger if that is a mob
// other than a slime. Vehicles that hand control to a player instead have
// a player as their first passenger, so a mob found here steers them.
func controllingMob(tx *world.Tx, vehicle world.Entity) (*Mob, bool) {
	v, ok := mobOf(vehicle)
	if !ok || v.AI.NoAI {
		return nil, false
	}
	first, ok := firstPassenger(tx, vehicle)
	if !ok || entityTypeInTag(first, "non_controlling_rider") {
		return nil, false
	}
	return mobOf(first)
}

// controlledVehicle ports Entity.getControlledVehicle for a mob: the mob it
// rides if it steers it.
func (m *Mob) controlledVehicle() (*Mob, bool) {
	if m.data.Riding == nil {
		return nil, false
	}
	vehicle, ok := m.data.Riding.Entity(m.tx)
	if !ok {
		return nil, false
	}
	controller, ok := controllingMob(m.tx, vehicle)
	if !ok || controller.H() != m.H() {
		return nil, false
	}
	return mobOf(vehicle)
}

// steer hands the navigation and move control of a rider to those of the
// vehicle it steers, or takes them back if vehicle is nil, as
// Mob.getNavigation and getMoveControl return those of the controlled
// vehicle.
func (ai *MobAI) steer(vehicle *Mob) {
	if vehicle == nil {
		ai.nav.vehicle, ai.move.vehicle = nil, nil
		return
	}
	ai.nav.vehicle, ai.move.vehicle = vehicle, &vehicle.AI.move
}

// updateControlFlags ports Mob.updateControlFlags: a mob steered by a mob
// riding it neither moves, jumps nor looks around of its own accord, and a
// mob in a boat does not jump.
func (ai *MobAI) updateControlFlags(m *Mob) {
	_, controlled := controllingMob(m.tx, m.Self)
	inBoat := false
	if m.data.Riding != nil {
		if vehicle, ok := m.data.Riding.Entity(m.tx); ok {
			_, inBoat = vehicle.(*Boat)
		}
	}
	ai.Goals.SetControlFlag(GoalMove, !controlled)
	ai.Goals.SetControlFlag(GoalJump, !controlled && !inBoat)
	ai.Goals.SetControlFlag(GoalLook, !controlled)
}

// mobStartRiding ports Entity.startRiding for a mob mounting another
// entity: vehicles that keep seats of their own seat it there, and the rest
// learn of it from the passenger riding them. announce sends the link to
// the viewers of the vehicle, which those spawning together with their
// vehicle do not need, as they are shown riding it.
func mobStartRiding(tx *world.Tx, rider, vehicle world.Entity, announce bool) bool {
	if rider.H().Type() == (PiglinType{}) && mobBaby(rider) && vehicle.H().Type() == HoglinType {
		// Piglin.startRiding: baby piglins stack up to three high on a hoglin.
		vehicle = topPassenger(tx, vehicle, 3)
	}
	r, ok := rider.(Rider)
	if !ok || r.Riding() != nil || rider.H() == vehicle.H() || rootVehicle(tx, vehicle) == rider.H() {
		return false
	}
	if v, ok := vehicle.(mobPassengerSeater); ok {
		return v.addMobPassenger(tx, rider, announce)
	}
	seatRider(r, vehicle)
	if announce {
		viewRidingLink(tx, vehicle, rider.H(), true)
	}
	return true
}

// topPassenger ports Piglin.getTopPassenger: the passenger counter-1 high
// in the stack riding vehicle, or the topmost below that.
func topPassenger(tx *world.Tx, vehicle world.Entity, counter int) world.Entity {
	if counter == 1 {
		return vehicle
	}
	if first, ok := firstPassenger(tx, vehicle); ok {
		return topPassenger(tx, first, counter-1)
	}
	return vehicle
}

// mobPassengerSeater is implemented by vehicles that keep the seats of the
// mobs riding them themselves.
type mobPassengerSeater interface {
	addMobPassenger(tx *world.Tx, passenger world.Entity, announce bool) bool
}

// mobStopRiding ports Entity.stopRiding for a mob.
func mobStopRiding(tx *world.Tx, rider world.Entity) {
	r, ok := rider.(Rider)
	if !ok || r.Riding() == nil {
		return
	}
	if DismountRider(rider, tx) && r.Riding() == nil {
		return
	}
	vehicle, ok := r.Riding().Entity(tx)
	r.SetRiding(nil)
	if ok {
		viewRidingLink(tx, vehicle, rider.H(), false)
	}
}

// viewRidingLink shows the viewers of a vehicle a passenger mounting or
// dismounting it.
func viewRidingLink(tx *world.Tx, vehicle world.Entity, rider *world.EntityHandle, mounted bool) {
	viewers := tx.Viewers(vehicle.Position())
	for _, v := range viewers {
		if lv, ok := v.(interface {
			ViewEntityLinkType(*world.EntityHandle, *world.EntityHandle, LinkType, bool)
		}); ok {
			lv.ViewEntityLinkType(vehicle.H(), rider, LinkRider, mounted)
		} else if lv, ok := v.(interface {
			ViewEntityLink(*world.EntityHandle, *world.EntityHandle, bool)
		}); ok {
			lv.ViewEntityLink(vehicle.H(), rider, mounted)
		}
	}
	tx.ReleaseViewers(viewers)
}

// passengerAttachment ports the passenger attachment of the types of
// EntityType mobs ride that keep no seats of their own: where on them their
// first passenger sits, or on top of them.
func passengerAttachment(vehicle world.Entity) mgl64.Vec3 {
	var p mgl64.Vec3
	switch vehicle.H().Type().EncodeEntity() {
	case "minecraft:ravager":
		p = mgl64.Vec3{0, 2.2625, -.0625}
	case "minecraft:hoglin", "minecraft:zoglin":
		p = mgl64.Vec3{0, 1.49375, 0}
	case "minecraft:piglin":
		p = mgl64.Vec3{0, 2.0125, 0}
	default:
		p = mgl64.Vec3{0, vehicle.H().Type().BBox(vehicle).Height(), 0}
	}
	if mobBaby(vehicle) {
		// Babies are half the size, and so is where they are ridden.
		p = p.Mul(.5)
	}
	sin, cos := math.Sincos(-vehicle.Rotation().Yaw() * math.Pi / 180)
	p[0], p[2] = p[0]*cos+p[2]*sin, p[2]*cos-p[0]*sin
	return p
}

// newMount ports the Mount behaviour: a mob with a ride target walks up to
// it and gets on once within a block.
func newMount(speed float64) *oneShot {
	return newOneShot([]memoryCondition{{memLookTarget, memRegistered}, {memWalkTarget, memAbsent}, {memRideTarget, memPresent}}, func(m *Mob, b *Brain, _ int64) bool {
		if m.data.Riding != nil {
			return false
		}
		target := b.entity(m, memRideTarget)
		if target == nil {
			return false
		}
		if target.Position().Sub(m.data.Pos).LenSqr() < 1 {
			mobStartRiding(m.tx, m.Self, target, true)
		} else {
			b.set(memLookTarget, newEntityTracker(target, true))
			b.set(memWalkTarget, walkTarget{target: newEntityTracker(target, false), speed: speed, closeEnough: 1})
		}
		return true
	})
}

// newDismountOrSkipMounting ports DismountOrSkipMounting: a mob gets off,
// or gives up getting on, a vehicle that is gone, more than maxDist away or
// for which stop returns true.
func newDismountOrSkipMounting(maxDist float64, stop func(m *Mob, vehicle world.Entity) bool) *oneShot {
	return newOneShot([]memoryCondition{{memRideTarget, memRegistered}}, func(m *Mob, b *Brain, _ int64) bool {
		var vehicle world.Entity
		if m.data.Riding != nil {
			vehicle, _ = m.data.Riding.Entity(m.tx)
		}
		if vehicle == nil {
			vehicle = b.entity(m, memRideTarget)
		}
		if vehicle == nil && m.data.Riding == nil && !b.has(memRideTarget) {
			return false
		}
		if vehicle != nil && entityAlive(vehicle) && vehicle.Position().Sub(m.data.Pos).LenSqr() < maxDist*maxDist && !stop(m, vehicle) {
			return false
		}
		mobStopRiding(m.tx, m.Self)
		b.erase(memRideTarget)
		return true
	})
}

// newTriggerOneShuffled ports TriggerGate.triggerOneShuffled: the first of
// triggers, shuffled by weight, that triggers.
func newTriggerOneShuffled(triggers ...weightedBehavior) *oneShot {
	g := newRunOne(nil, triggers...)
	return newOneShot(nil, func(m *Mob, _ *Brain, time int64) bool {
		g.tryStart(m, time)
		started := false
		for _, c := range g.children {
			if c.b.status() == behaviorRunning {
				started = true
				c.b.doStop(m, time)
			}
		}
		g.st = behaviorStopped
		return started
	})
}

// StartRiding makes a mob start riding a vehicle, as Entity.startRiding:
// a mob rider steers a mob it rides.
func StartRiding(tx *world.Tx, rider, vehicle world.Entity) bool {
	return mobStartRiding(tx, rider, vehicle, true)
}
