package biome

import "github.com/df-mc/dragonfly/server/world/generator/pmgen/populate"

type Plains struct {
	grassy
}

func (Plains) Populators() []populate.Populator {
	return []populate.Populator{populate.TallGrass{Amount: 12}}
}

func (Plains) ID() uint8 {
	return IDPlains
}

func (Plains) Elevation() (min, max int) {
	return 63, 68
}

func (Plains) Temperature() float64 {
	return 0.8
}

func (Plains) Rainfall() float64 {
	return 0.4
}
