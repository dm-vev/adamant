package entity

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
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

// VehicleInput represents an entity that can be controlled by rider input.
type VehicleInput interface {
	SetVehicleInput(strafe, forward float64)
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
