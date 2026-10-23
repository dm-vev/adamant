package end

// BiomeAt samples the generator's Java biome source without generating a chunk.
func (g *End) BiomeAt(x, y, z int32) string {
	return g.biomeAt(int(x), int(y), int(z))
}
