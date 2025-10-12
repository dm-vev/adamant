package biome

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/pmgen/populate"
)

type River struct{}

func (River) Populators() []populate.Populator {
	return []populate.Populator{populate.TallGrass{Amount: 5}}
}

func (River) ID() uint8 {
	return IDRiver
}

func (River) Elevation() (min, max int) {
	return 58, 62
}

func (River) GroundCover() []world.Block {
	return []world.Block{
		block.Dirt{},
		block.Dirt{},
		block.Dirt{},
		block.Dirt{},
		block.Dirt{},
	}
}

func (River) Temperature() float64 {
	return 0.5
}

func (River) Rainfall() float64 {
	return 0.7
}
