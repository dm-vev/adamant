package biome

import "github.com/df-mc/dragonfly/server/world/generator/pmgen/populate"

type Swamp struct {
	grassy
}

func (Swamp) Populators() []populate.Populator {
	return nil
}

func (Swamp) ID() uint8 {
	return IDSwamp
}

func (Swamp) Elevation() (min, max int) {
	return 62, 63
}

func (Swamp) Temperature() float64 {
	return 0.8
}

func (Swamp) Rainfall() float64 {
	return 0.9
}
