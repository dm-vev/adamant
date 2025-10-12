package biome

import "github.com/df-mc/dragonfly/server/world/generator/pmgen/populate"

type Desert struct {
	sandy
}

func (Desert) Populators() []populate.Populator {
	return nil
}

func (Desert) ID() uint8 {
	return IDDesert
}

func (Desert) Elevation() (min, max int) {
	return 63, 74
}

func (Desert) Temperature() float64 {
	return 2.0
}

func (Desert) Rainfall() float64 {
	return 0.0
}
