package overworld

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
)

func (g *Overworld) populateChunk(tx *world.Tx, pos world.ChunkPos) {
	chunkX, chunkZ := int(pos[0]), int(pos[1])

	r := g.chunkPopulationRand(chunkX, chunkZ)

	origin := cube.Pos{chunkX * 16, 0, chunkZ * 16}
	biomeID := g.biomeIDAt(origin[0]+8, origin[2]+8)

	g.populateDungeons(tx, r, chunkX, chunkZ, origin)
	g.populateOres(tx, r, chunkX, chunkZ, origin, biomeID)
}

func (g *Overworld) chunkPopulationRand(chunkX, chunkZ int) *mc112.Rand {
	// Matches ChunkGeneratorOverworld.populate seeding in Java 1.12.
	r := mc112.NewRand(g.seed)
	j := r.Long()/2*2 + 1
	k := r.Long()/2*2 + 1
	r.SetSeed(int64(chunkX)*j + int64(chunkZ)*k ^ g.seed)
	return r
}
