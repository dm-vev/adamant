package overworld

// JavaBiomeAt samples the generator's Java biome source without generating a chunk.
func (g *Overworld) JavaBiomeAt(x, y, z int32) string {
	return (modernTerrainSource{generator: g}).JavaBiomeAt(x, y, z)
}
