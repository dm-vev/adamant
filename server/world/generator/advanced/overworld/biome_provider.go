package overworld

import "github.com/df-mc/dragonfly/server/world/generator/advanced/overworld/internal/genlayer"

type biomeProvider struct {
	genBiomes   genlayer.Layer
	biomeIndex  genlayer.Layer
	largeBiomes bool
}

func newBiomeProvider(seed int64) *biomeProvider {
	stack := genlayer.NewStack112(seed, false)
	return &biomeProvider{
		genBiomes:  stack.GenBiomes,
		biomeIndex: stack.BiomeIndex,
	}
}

func (p *biomeProvider) biomesForGeneration(x, z, w, h int) []int {
	return p.genBiomes.GetInts(x, z, w, h)
}

func (p *biomeProvider) biomes(x, z, w, h int) []int {
	return p.biomeIndex.GetInts(x, z, w, h)
}
