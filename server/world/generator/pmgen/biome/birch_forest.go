package biome

import "github.com/df-mc/dragonfly/server/world/generator/pmgen/populate"

type BirchForest struct {
	grassy
}

func (BirchForest) Populators() []populate.Populator {
	return []populate.Populator{
		populate.Tree{BaseAmount: 10, Type: populate.BirchTree{}},
	}
}

func (BirchForest) ID() uint8 {
	return IDBirchForest
}

func (BirchForest) Elevation() (min, max int) {
	return 60, 70
}

func (BirchForest) Temperature() float64 {
	return 0.6
}

func (BirchForest) Rainfall() float64 {
	return 0.6
}
