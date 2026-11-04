package nether

import "github.com/df-mc/dragonfly/server/internal/vanilla"

// Only tests use these helpers of nether.go, so they live with the tests.

func (g *Nether) biomeIndexAt(x, y, z int32) int {
	quartX, _, quartZ := vanilla.FiddledBiomeQuart(g.biomeZoomSeed, x, y, z)
	return netherBiomeIndex(g.biomes.Biome(quartX*4, quartZ*4))
}
