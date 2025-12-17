package overworld

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func (g *Overworld) initCarving() {
	g.carvable = make(map[uint32]struct{}, 128)
	add := func(rid uint32) {
		g.carvable[rid] = struct{}{}
	}

	add(g.stoneRID)
	add(g.dirtRID)
	add(world.BlockRuntimeID(block.Dirt{Coarse: true}))
	add(g.grassRID)
	add(g.myceliumRID)
	add(g.podzolRID)
	add(g.gravelRID)

	add(g.sandRID)
	add(g.redSandRID)

	for _, t := range block.SandstoneTypes() {
		add(world.BlockRuntimeID(block.Sandstone{Type: t}))
		add(world.BlockRuntimeID(block.Sandstone{Type: t, Red: true}))
	}

	add(g.terracottaRID)
	for _, c := range item.Colours() {
		add(world.BlockRuntimeID(block.StainedTerracotta{Colour: c}))
	}
}
