package biome

import "github.com/df-mc/dragonfly/server/world/generator/pmgen/populate"

type SmallMountains struct {
	grassy
}

func (SmallMountains) Populators() []populate.Populator {
	return nil
}

func (SmallMountains) ID() uint8 {
	return IDMountains
}

func (SmallMountains) Elevation() (min, max int) {
	return 63, 97
}

func (SmallMountains) Temperature() float64 {
	return 0.4
}

func (SmallMountains) Rainfall() float64 {
	return 0.5
}
