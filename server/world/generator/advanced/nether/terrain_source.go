package nether

// baseColumn matches NoiseBasedChunkGenerator.getBaseColumn before surface
// rules, carvers and structures. Values are native registry IDs; the Java noise
// settings cover 0..127 even when the level-height accessor extends to 255.
func (g *Nether) baseColumn(x, z int32, column *[128]uint32) {
	var density [128]float32
	g.density.Column(x, z, &density)
	for y, value := range density {
		switch {
		case value > 0:
			column[y] = g.netherrackRID
		case y < netherSeaLevel:
			column[y] = g.lavaStillRID
		default:
			column[y] = g.airRID
		}
	}
}
