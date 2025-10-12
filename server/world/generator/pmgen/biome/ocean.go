package biome

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/pmgen/populate"
)

type Ocean struct{}

func (Ocean) Populators() []populate.Populator {
	return []populate.Populator{populate.TallGrass{Amount: 5}}
}

func (Ocean) ID() uint8 {
	return IDOcean
}

func (Ocean) Elevation() (min, max int) {
	return 46, 58
}

func (Ocean) GroundCover() []world.Block {
	return []world.Block{
		block.Gravel{},
		block.Gravel{},
		block.Gravel{},
		block.Gravel{},
		block.Gravel{},
	}
}

func (Ocean) Temperature() float64 {
	return 0.5
}

func (Ocean) Rainfall() float64 {
	return 0.5
}
