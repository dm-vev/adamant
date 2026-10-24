package entity

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Interactable represents an entity that can be interacted with by a user.
type Interactable interface {
	Interact(tx *world.Tx, user item.User, ctx *item.UseContext) bool
}

// Rider represents an entity that can ride another entity.
type Rider interface {
	Riding() *world.EntityHandle
	SetRiding(*world.EntityHandle)
}

// passengerPositioner supplies the vehicle's source attachment minus the
// passenger's attachment. Locomotion uses it instead of independent physics.
type passengerPositioner interface {
	PassengerPosition(passenger world.Entity) mgl64.Vec3
}

func vehicleAttachmentPoint(passenger world.Entity) mgl64.Vec3 {
	if attached, ok := passenger.(interface{ VehicleAttachmentPoint() mgl64.Vec3 }); ok {
		return attached.VehicleAttachmentPoint()
	}
	return mgl64.Vec3{}
}

// VehicleInput represents an entity that can be controlled by rider input.
type VehicleInput interface {
	SetVehicleInput(strafe, forward float64)
}

// VehicleInputWithRider is an optional input extension for vehicles whose
// steering depends on the controlling passenger's orientation.
type VehicleInputWithRider interface {
	SetVehicleInputFrom(passenger world.Entity, strafe, forward float64)
}

// VehicleJumpInput is an optional input extension for vehicles that react
// to the controlling passenger jumping, such as a happy ghast flying up.
type VehicleJumpInput interface {
	SetVehicleJumping(jumping bool)
}

// VehicleController represents a vehicle that may decide which passenger controls it.
type VehicleController interface {
	ControlsVehicle(passenger world.Entity) bool
}

// PassengerDismounter removes a specific passenger from a vehicle.
type PassengerDismounter interface {
	Dismount(tx *world.Tx, passenger world.Entity)
}

// DismountRider removes rider from its current vehicle. It returns whether the
// vehicle exposed a passenger dismount operation.
func DismountRider(rider world.Entity, tx *world.Tx) bool {
	r, ok := rider.(Rider)
	if !ok || r.Riding() == nil {
		return false
	}
	vehicle, ok := r.Riding().Entity(tx)
	if !ok {
		r.SetRiding(nil)
		return false
	}
	d, ok := vehicle.(PassengerDismounter)
	if !ok {
		return false
	}
	d.Dismount(tx, rider)
	return true
}

// PiercingWeaponTarget can override the default eligibility of an entity for
// a piercing weapon attack.
type PiercingWeaponTarget interface {
	CanBeHitByPiercingWeapon(attacker world.Entity) bool
}

// Harnessable is implemented by mobs that can wear a harness.
type Harnessable interface {
	CanEquipHarness(user world.Entity) bool
	EquipHarness(tx *world.Tx, user world.Entity, harness world.Item) bool
	ShearHarness(tx *world.Tx, user world.Entity) bool
}

// BodyArmorable is implemented by mobs that can wear body armour.
type BodyArmorable interface {
	CanEquipBodyArmor(user world.Entity) bool
	EquipBodyArmor(tx *world.Tx, user world.Entity, armour world.Item) bool
}

// WolfTameable is implemented by wolves that can be interacted with using
// bones. TameWithBone reports whether the bone should be consumed, including
// unsuccessful taming attempts and successful feeding/healing.
type WolfTameable interface {
	TameWithBone(tx *world.Tx, user world.Entity) bool
}

// Milkable is implemented by adult entities that can be milked with an empty bucket.
type Milkable interface {
	Milk(tx *world.Tx) bool
	MilkSound() world.Sound
}

// Bucketable exposes entity pickup state stored by mob buckets.
type Bucketable interface {
	CanBePickedUpWithBucket(bucket item.Bucket) bool
	BucketItem() item.Stack
	SaveToBucket(bucket item.Stack) item.Stack
	LoadFromBucket(bucket item.Stack)
	BucketPickupSound() world.Sound
	FromBucket() bool
	SetFromBucket(bool)
}

// CanBeHitByPiercingWeapon reports whether target can be affected by a
// piercing weapon used by attacker.
func CanBeHitByPiercingWeapon(target, attacker world.Entity) bool {
	if target == nil || attacker == nil || target.H() == attacker.H() {
		return false
	}
	living, ok := target.(Living)
	if !ok || living.Dead() {
		return false
	}
	if t, ok := target.(PiercingWeaponTarget); ok && !t.CanBeHitByPiercingWeapon(attacker) {
		return false
	}
	targetRider, targetRides := target.(Rider)
	attackerRider, attackerRides := attacker.(Rider)
	if targetRides && attackerRides {
		if vehicle := targetRider.Riding(); vehicle != nil && vehicle == attackerRider.Riding() {
			return false
		}
	}
	return true
}

// ContainerOpener represents a user that can open an entity container.
type ContainerOpener interface {
	OpenEntityContainer(e world.Entity, inv *inventory.Inventory, containerType byte, tx *world.Tx)
}

// LinkType is the type of link between two entities.
type LinkType byte

const (
	LinkRemove LinkType = iota
	LinkRider
	LinkPassenger
)

// Link represents a link between two entities for riding.
type Link struct {
	Ridden                 *world.EntityHandle
	Rider                  *world.EntityHandle
	Type                   LinkType
	Immediate              bool
	RiderInitiated         bool
	VehicleAngularVelocity float32
}

// Linker represents an entity that exposes active links.
type Linker interface {
	Links() []Link
}
