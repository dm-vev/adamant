package overworld

// BiomeAt samples the generator's Java biome source without generating a chunk.
func (g *Overworld) BiomeAt(x, y, z int32) string {
	return (modernTerrainSource{generator: g}).BiomeAt(x, y, z)
}
