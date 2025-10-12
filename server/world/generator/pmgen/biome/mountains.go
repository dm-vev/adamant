package biome

import "github.com/df-mc/dragonfly/server/world/generator/pmgen/populate"

type Mountains struct {
	grassy
}

func (Mountains) Populators() []populate.Populator {
	return nil
}

func (Mountains) ID() uint8 {
	return IDMountains
}

func (Mountains) Elevation() (min, max int) {
	return 63, 127
}

func (Mountains) Temperature() float64 {
	return 0.4
}

func (Mountains) Rainfall() float64 {
	return 0.5
}
