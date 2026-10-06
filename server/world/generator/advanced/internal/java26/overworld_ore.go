package java26

// OreVein supplies the four NoiseRouterData material-rule inputs for a block in
// this chunk. It does no positional random draws: OreVeinRule owns that stream.
func (c *OverworldChunk) OreVein(x, y, z int32) (copperDensity, ironDensity, richness, gap float32) {
	copperDensity, ironDensity = -1, -1
	if y < -60 || y >= 50 || y >= -8 && y < 0 {
		return
	}
	x, z = x-c.baseX, z-c.baseZ
	toggle := c.interpolateField(x, y, z, cellVeininess)
	if toggle >= -0.4 && toggle < 0.4 {
		return
	}
	veinA := terrainAbs(c.interpolateField(x, y, z, cellVeinA))
	veinB := terrainAbs(c.interpolateField(x, y, z, cellVeinB))
	if max(veinA, veinB) > 0.08 {
		return
	}
	minimum, maximum, veininess := int32(-60), int32(-8), -toggle
	if toggle > 0 {
		minimum, maximum, veininess = 0, 50, toggle
	}
	if y < minimum || y >= maximum {
		return
	}
	edgeRoundoff := mapClamped(float32(min(maximum-y, y-minimum)), 0, 20, -0.2, 0)
	if veininess-0.4+edgeRoundoff < 0 {
		return
	}
	if toggle > 0 {
		copperDensity = 0.7
	} else {
		ironDensity = 0.7
	}
	richness = mapClamped(terrainAbs(toggle), 0.4, 0.6, 0.1, 0.3)
	gap = -0.3 - c.noise.veinGap.Sample(float64(x+c.baseX), float64(y), float64(z+c.baseZ))
	return
}

func (c *OverworldChunk) interpolateField(x, y, z int32, field int) float32 {
	ix, iy, iz := x>>2, (y+64)>>3, z>>2
	dx, dy, dz := float32(x&3)*0.25, float32(y&7)*0.125, float32(z&3)*0.25
	a := terrainLerp(dy, c.cells[ix][iz][iy][field], c.cells[ix][iz][iy+1][field])
	b := terrainLerp(dy, c.cells[ix+1][iz][iy][field], c.cells[ix+1][iz][iy+1][field])
	d := terrainLerp(dy, c.cells[ix][iz+1][iy][field], c.cells[ix][iz+1][iy+1][field])
	e := terrainLerp(dy, c.cells[ix+1][iz+1][iy][field], c.cells[ix+1][iz+1][iy+1][field])
	return terrainLerp(dz, terrainLerp(dx, a, b), terrainLerp(dx, d, e))
}

func mapClamped(value, fromMinimum, fromMaximum, toMinimum, toMaximum float32) float32 {
	factor := (toMaximum - toMinimum) / (fromMaximum - fromMinimum)
	offset := toMinimum - fromMinimum*factor
	return clampDensity(value, fromMinimum, fromMaximum)*factor + offset
}
