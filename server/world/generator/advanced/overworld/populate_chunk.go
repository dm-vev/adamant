package overworld

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
)

var populationSetOpts = &world.SetOpts{DisableBlockUpdates: true, DisableLiquidDisplacement: true}

func (g *Overworld) chunkPopulationRand(chunkX, chunkZ int) *mc112.Rand {
	// Matches ChunkGeneratorOverworld.populate seeding in Java 1.12.
	r := mc112.NewRand(g.seed)
	j := r.Long()/2*2 + 1
	k := r.Long()/2*2 + 1
	r.SetSeed(int64(chunkX)*j + int64(chunkZ)*k ^ g.seed)
	return r
}
