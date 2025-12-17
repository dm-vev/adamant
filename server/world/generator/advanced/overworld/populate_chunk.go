package overworld

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
)

func (g *Overworld) populateChunk(tx *world.Tx, pos world.ChunkPos) {
	chunkX, chunkZ := int(pos[0]), int(pos[1])

	// Java 1.12 decorations frequently access positions in a 1-chunk radius due to the +8 offset used for
	// feature placement.
	g.ensurePopulationArea(tx, chunkX, chunkZ)

	r := g.chunkPopulationRand(chunkX, chunkZ)

	// In Java 1.12 biome.decorate uses the biome at (chunkX*16+16, chunkZ*16+16), matching the +8 feature offset.
	origin := cube.Pos{chunkX * 16, 0, chunkZ * 16}
	biomeID := g.biomeProvider.biomes(origin[0]+16, origin[2]+16, 1, 1)[0]

	g.populateDungeons(tx, r, origin)
	g.populateOres(tx, r, origin, biomeID)
}

func (g *Overworld) ensurePopulationArea(tx *world.Tx, chunkX, chunkZ int) {
	y := tx.Range().Min()
	if y < 0 {
		y = 0
	}
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			tx.Block(cube.Pos{(chunkX + dx) * 16, y, (chunkZ + dz) * 16})
		}
	}
}

func (g *Overworld) chunkPopulationRand(chunkX, chunkZ int) *mc112.Rand {
	// Matches ChunkGeneratorOverworld.populate seeding in Java 1.12.
	r := mc112.NewRand(g.seed)
	j := r.Long()/2*2 + 1
	k := r.Long()/2*2 + 1
	r.SetSeed(int64(chunkX)*j + int64(chunkZ)*k ^ g.seed)
	return r
}
