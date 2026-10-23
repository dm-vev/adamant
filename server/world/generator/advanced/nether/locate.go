package nether

// BiomeAt samples the generator's Java biome source without generating a chunk.
func (g *Nether) BiomeAt(x, y, z int32) string {
	return g.featureSource.BiomeAt(x, y, z)
}
