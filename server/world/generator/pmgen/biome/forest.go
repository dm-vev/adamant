package biome

import "github.com/df-mc/dragonfly/server/world/generator/pmgen/populate"

type Forest struct {
	grassy
}

func (Forest) Populators() []populate.Populator {
	return []populate.Populator{
		populate.Tree{Type: populate.OakTree{}, BaseAmount: 5},
		populate.TallGrass{Amount: 3},
	}
}

func (Forest) ID() uint8 {
	return IDForest
}

func (Forest) Elevation() (min, max int) {
	return 63, 81
}

func (Forest) Temperature() float64 {
	return 0.7
}

func (Forest) Rainfall() float64 {
	return 0.8
}
