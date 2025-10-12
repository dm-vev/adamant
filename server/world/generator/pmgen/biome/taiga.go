package biome

import "github.com/df-mc/dragonfly/server/world/generator/pmgen/populate"

type Taiga struct {
	snowy
}

func (Taiga) Populators() []populate.Populator {
	return []populate.Populator{
		populate.Tree{Type: populate.SpruceTree{}, BaseAmount: 10},
		populate.TallGrass{Amount: 1},
	}
}

func (Taiga) ID() uint8 {
	return IDTaiga
}

func (Taiga) Elevation() (min, max int) {
	return 63, 81
}

func (Taiga) Temperature() float64 {
	return 0.05
}

func (Taiga) Rainfall() float64 {
	return 0.8
}
