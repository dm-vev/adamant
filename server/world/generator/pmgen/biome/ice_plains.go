package biome

import "github.com/df-mc/dragonfly/server/world/generator/pmgen/populate"

type IcePlains struct {
	snowy
}

func (IcePlains) Populators() []populate.Populator {
	return []populate.Populator{populate.TallGrass{Amount: 5}}
}

func (IcePlains) ID() uint8 {
	return IDIcePlains
}

func (IcePlains) Elevation() (min, max int) {
	return 63, 74
}

func (IcePlains) Temperature() float64 {
	return 0.05
}

func (IcePlains) Rainfall() float64 {
	return 0.8
}
