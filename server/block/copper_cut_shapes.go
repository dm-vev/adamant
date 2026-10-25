package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// RandomTick ports WeatheringCopperSlabBlock.randomTick: cut copper slabs
// oxidise as copper blocks do.
func (s Slab) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	if s.CanOxidate() {
		attemptOxidation(pos, tx, r, s)
	}
}

// CanOxidate reports if the slab is of unwaxed copper.
func (s Slab) CanOxidate() bool {
	c, ok := s.Block.(Copper)
	return ok && !c.Waxed
}

// OxidationLevel returns the oxidation of a copper slab.
func (s Slab) OxidationLevel() OxidationType {
	c, _ := s.Block.(Copper)
	return c.Oxidation
}

// WithOxidationLevel returns the slab with its copper at the oxidation passed.
func (s Slab) WithOxidationLevel(o OxidationType) Oxidisable {
	if c, ok := s.Block.(Copper); ok {
		c.Oxidation = o
		s.Block = c
	}
	return s
}

// RandomTick ports WeatheringCopperStairBlock.randomTick: cut copper stairs
// oxidise as copper blocks do.
func (s Stairs) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	if s.CanOxidate() {
		attemptOxidation(pos, tx, r, s)
	}
}

// CanOxidate reports if the stairs are of unwaxed copper.
func (s Stairs) CanOxidate() bool {
	c, ok := s.Block.(Copper)
	return ok && !c.Waxed
}

// OxidationLevel returns the oxidation of copper stairs.
func (s Stairs) OxidationLevel() OxidationType {
	c, _ := s.Block.(Copper)
	return c.Oxidation
}

// WithOxidationLevel returns the stairs with their copper at the oxidation
// passed.
func (s Stairs) WithOxidationLevel(o OxidationType) Oxidisable {
	if c, ok := s.Block.(Copper); ok {
		c.Oxidation = o
		s.Block = c
	}
	return s
}

// RandomlyTicks reports if the slab ticks, as only unwaxed copper slabs do.
func (s Slab) RandomlyTicks() bool { return s.CanOxidate() }

// RandomlyTicks reports if the stairs tick, as only unwaxed copper stairs do.
func (s Stairs) RandomlyTicks() bool { return s.CanOxidate() }
