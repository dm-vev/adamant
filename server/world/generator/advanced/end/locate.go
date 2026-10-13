package end

// JavaBiomeAt samples the generator's Java biome source without generating a chunk.
func (g *End) JavaBiomeAt(x, y, z int32) string {
	return g.javaBiomeAt(int(x), int(y), int(z))
}
