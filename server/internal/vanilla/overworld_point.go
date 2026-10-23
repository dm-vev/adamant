package vanilla

// SampleDensity evaluates the same 4x8 interpolated graph without constructing a
// chunk. Structure height probes use this path; chunk filling reuses its lattice.
func (n *OverworldNoise) SampleDensity(x, y, z int32) float32 {
	baseX, baseY, baseZ := x&^3, y&^7, z&^3
	var cells [8]overworldDensityCell
	for dx := range 2 {
		for dz := range 2 {
			wx, wz := baseX+int32(dx)*4, baseZ+int32(dz)*4
			var column OverworldColumn
			if baseY < 256 && baseY+8 > -64 {
				column, _, _ = n.shapeColumn(wx, wz)
				n.applyJaggedness(&column, wx, wz)
			}
			index := dz*4 + dx*2
			cells[index] = n.sampleDensityCell(wx, baseY, wz, column, false)
			cells[index+1] = n.sampleDensityCell(wx, baseY+8, wz, column, false)
		}
	}
	return interpolateOverworldCells(&cells[0], &cells[1], &cells[2], &cells[3], &cells[4], &cells[5], &cells[6], &cells[7], float32(x&3)*0.25, float32(y&7)*0.125, float32(z&3)*0.25)
}
