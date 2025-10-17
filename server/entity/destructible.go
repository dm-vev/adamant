package entity

import "github.com/df-mc/dragonfly/server/world"

// Destructible represents an entity that may be destroyed by attacks or other damage sources.
type Destructible interface {
	world.Entity
	// Destroy destroys the entity using the world.Tx provided. The damage source and the entity that caused
	// the destruction are passed for context. Destroy should return true if the entity was actually destroyed
	// as a result of the call.
	Destroy(tx *world.Tx, src world.DamageSource, causer world.Entity) bool
}
