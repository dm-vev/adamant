package vanilla

import "math"

// OverworldChunk is reusable, worker-local storage for Java's 4x8 density cells
// and biome-manager quart neighbourhood. Prepare overwrites every entry.
type OverworldChunk struct {
	noise        *OverworldNoise
	zoomSeed     int64
	baseX, baseZ int32
	columns      [6][6]OverworldColumn
	biomes       [6][6][98]overworldBiomeCell
	cells        [5][5][49]overworldDensityCell
	preliminary  [2][2]float32
}

type overworldBiomeCell struct {
	fiddle      [3]float64
	index       uint8
	fiddleValid bool
}

type overworldDensityCell [8]float32

const (
	cellDensity = iota
	cellNoodle
	cellNoodleThickness
	cellNoodleA
	cellNoodleB
	cellVeininess
	cellVeinA
	cellVeinB
)

// QuartBiomes returns the index, as OverworldBiomeName names it, of the noise
// biome at a quart with quartY within -16..79: the biome FindOverworldBiome
// gives for the climate there.
type QuartBiomes func(quartX, quartY, quartZ int32) uint8

// PrepareRaw fills terrain cells and preliminary heights without populating
// biome Voronoi grids. Used by the raw structure-probe cache.
func (c *OverworldChunk) PrepareRaw(noise *OverworldNoise, baseX, baseZ int32) {
	c.noise, c.baseX, c.baseZ = noise, baseX, baseZ
	for cx := range 5 {
		wx := baseX + int32(cx)*4
		col := &c.columns[cx+1]
		for cz := range 5 {
			col[cz+1] = noise.SampleTerrainColumn(wx, baseZ+int32(cz)*4)
		}
	}
	c.prepareCells(noise, baseX, baseZ, false)
	for x := range 2 {
		for z := range 2 {
			c.preliminary[x][z] = float32(preliminarySurfaceHeight(c.columns[1+x*4][1+z*4]))
		}
	}
}

// Prepare samples one complete -64..319 chunk plus interpolation/biome borders.
// zoomSeed is the world seed transformed once with BiomeZoomSeed. The noise
// biomes are read from biomes if it is not nil.
func (c *OverworldChunk) Prepare(noise *OverworldNoise, zoomSeed int64, baseX, baseZ int32, biomes QuartBiomes) {
	c.noise, c.zoomSeed, c.baseX, c.baseZ = noise, zoomSeed, baseX, baseZ
	var hint ClimateHint
	for x := range 6 {
		wx := baseX + int32(x-1)*4
		for z := range 6 {
			wz := baseZ + int32(z-1)*4
			if biomes != nil {
				if x > 0 && z > 0 {
					c.columns[x][z] = noise.SampleTerrainColumn(wx, wz)
				}
				for y := range 98 {
					quartY := int32(y - 17)
					clampedY := max(-16, min(quartY, 79))
					cell := &c.biomes[x][z][y]
					cell.index = biomes(wx>>2, clampedY, wz>>2)
					cell.fiddleValid = false
				}
			} else {
				column := noise.SampleColumn(wx, wz)
				c.columns[x][z] = column
				var colBiomes [96]uint8
				FillOverworldBiomeColumn(column, &colBiomes, &hint)
				for y := range 98 {
					quartY := int32(y - 17)
					clampedY := max(-16, min(quartY, 79))
					cell := &c.biomes[x][z][y]
					cell.index = colBiomes[clampedY+16]
					cell.fiddleValid = false
				}
			}
		}
	}
	c.prepareCells(noise, baseX, baseZ, biomes != nil)
	for x := range 2 {
		for z := range 2 {
			c.preliminary[x][z] = float32(preliminarySurfaceHeight(c.columns[1+x*4][1+z*4]))
		}
	}
}

const noodleCellActiveThreshold float32 = -1e-6

func (c *OverworldChunk) prepareCells(noise *OverworldNoise, baseX, baseZ int32, includeOre bool) {
	var needNoodle [5][5][49]bool
	var needVein [5][5][16]bool
	for cx := range 5 {
		wx := baseX + int32(cx)*4
		col := &c.columns[cx+1]
		for cz := range 5 {
			wz := baseZ + int32(cz)*4
			column := col[cz+1]
			nx, nz := float64(wx), float64(wz)
			var elev float32
			var hasElev bool
			for cy := range 49 {
				wy := int32(cy)*8 - 64
				cell := &c.cells[cx][cz][cy]
				cell[cellDensity] = noise.rawDensityColumn(wx, wy, wz, column, &elev, &hasElev)
				cell[cellNoodle] = -1
				cell[cellNoodleThickness] = 0
				cell[cellNoodleA] = 0
				cell[cellNoodleB] = 0
				cell[cellVeininess] = 0
				cell[cellVeinA] = 1
				cell[cellVeinB] = 1
				if wy >= -60 && wy <= 320 {
					cell[cellNoodle] = noise.noodle.Sample(nx, float64(wy), nz)
				}
				if includeOre && wy >= -64 && wy <= 56 {
					cell[cellVeininess] = noise.veininess.Sample(nx*1.5, float64(wy)*1.5, nz*1.5)
				}
			}
		}
	}
	c.markActiveCells(&needNoodle, &needVein, includeOre)
	c.sampleLazyCornerDetails(noise, baseX, baseZ, &needNoodle, &needVein, includeOre)
}

func (c *OverworldChunk) markActiveCells(needNoodle *[5][5][49]bool, needVein *[5][5][16]bool, includeOre bool) {
	for cx := range 4 {
		for cz := range 4 {
			for cy := range 48 {
				if c.cells[cx][cz][cy][cellNoodle] >= noodleCellActiveThreshold || c.cells[cx][cz][cy+1][cellNoodle] >= noodleCellActiveThreshold ||
					c.cells[cx+1][cz][cy][cellNoodle] >= noodleCellActiveThreshold || c.cells[cx+1][cz][cy+1][cellNoodle] >= noodleCellActiveThreshold ||
					c.cells[cx][cz+1][cy][cellNoodle] >= noodleCellActiveThreshold || c.cells[cx][cz+1][cy+1][cellNoodle] >= noodleCellActiveThreshold ||
					c.cells[cx+1][cz+1][cy][cellNoodle] >= noodleCellActiveThreshold || c.cells[cx+1][cz+1][cy+1][cellNoodle] >= noodleCellActiveThreshold {
					for dx := range 2 {
						for dz := range 2 {
							needNoodle[cx+dx][cz+dz][cy] = true
							needNoodle[cx+dx][cz+dz][cy+1] = true
						}
					}
				}
			}
			if !includeOre {
				continue
			}
			for cy := range 15 {
				if cy == 7 {
					continue
				}
				v0 := c.cells[cx][cz][cy][cellVeininess]
				v1 := c.cells[cx][cz][cy+1][cellVeininess]
				v2 := c.cells[cx+1][cz][cy][cellVeininess]
				v3 := c.cells[cx+1][cz][cy+1][cellVeininess]
				v4 := c.cells[cx][cz+1][cy][cellVeininess]
				v5 := c.cells[cx][cz+1][cy+1][cellVeininess]
				v6 := c.cells[cx+1][cz+1][cy][cellVeininess]
				v7 := c.cells[cx+1][cz+1][cy+1][cellVeininess]
				if v0 < -0.399 || v0 >= 0.399 || v1 < -0.399 || v1 >= 0.399 ||
					v2 < -0.399 || v2 >= 0.399 || v3 < -0.399 || v3 >= 0.399 ||
					v4 < -0.399 || v4 >= 0.399 || v5 < -0.399 || v5 >= 0.399 ||
					v6 < -0.399 || v6 >= 0.399 || v7 < -0.399 || v7 >= 0.399 {
					for dx := range 2 {
						for dz := range 2 {
							needVein[cx+dx][cz+dz][cy] = true
							needVein[cx+dx][cz+dz][cy+1] = true
						}
					}
				}
			}
		}
	}
}

func (c *OverworldChunk) sampleLazyCornerDetails(noise *OverworldNoise, baseX, baseZ int32, needNoodle *[5][5][49]bool, needVein *[5][5][16]bool, includeOre bool) {
	const frequency = 2.6666666666666665
	for cx := range 5 {
		nx := float64(baseX + int32(cx)*4)
		for cz := range 5 {
			nz := float64(baseZ + int32(cz)*4)
			for cy := 1; cy < 49; cy++ {
				if !needNoodle[cx][cz][cy] {
					continue
				}
				ny := float64(int32(cy)*8 - 64)
				cell := &c.cells[cx][cz][cy]
				cell[cellNoodleThickness] = mapNoise(noise.noodleThickness.Sample(nx, ny, nz), -0.05, -0.1)
				cell[cellNoodleA] = noise.noodleRidgeA.Sample(nx*frequency, ny*frequency, nz*frequency)
				cell[cellNoodleB] = noise.noodleRidgeB.Sample(nx*frequency, ny*frequency, nz*frequency)
			}
			if !includeOre {
				continue
			}
			for cy := range 16 {
				if !needVein[cx][cz][cy] {
					continue
				}
				ny := float64(int32(cy)*8 - 64)
				cell := &c.cells[cx][cz][cy]
				cell[cellVeinA] = noise.veinA.Sample(nx*4, ny*4, nz*4)
				cell[cellVeinB] = noise.veinB.Sample(nx*4, ny*4, nz*4)
			}
		}
	}
}

func (n *OverworldNoise) sampleDensityCell(x, y, z int32, column OverworldColumn, includeOre bool) overworldDensityCell {
	result := overworldDensityCell{cellDensity: n.rawDensity(x, y, z, column), cellNoodle: -1, cellVeinA: 1, cellVeinB: 1}
	nx, ny, nz := float64(x), float64(y), float64(z)
	if y >= -60 && y <= 320 {
		result[cellNoodle] = n.noodle.Sample(nx, ny, nz)
		result[cellNoodleThickness] = mapNoise(n.noodleThickness.Sample(nx, ny, nz), -0.05, -0.1)
		const frequency = 2.6666666666666665
		result[cellNoodleA] = n.noodleRidgeA.Sample(nx*frequency, ny*frequency, nz*frequency)
		result[cellNoodleB] = n.noodleRidgeB.Sample(nx*frequency, ny*frequency, nz*frequency)
	}
	if includeOre && y >= -64 && y <= 56 {
		result[cellVeininess] = n.veininess.Sample(nx*1.5, ny*1.5, nz*1.5)
		result[cellVeinA] = n.veinA.Sample(nx*4, ny*4, nz*4)
		result[cellVeinB] = n.veinB.Sample(nx*4, ny*4, nz*4)
	}
	return result
}

func finalDensityFromRaw(raw float32) float32 {
	value := clampDensity(raw, -1, 1)
	return value*0.5 - value*value*value/24
}

// FillSubChunkDensities fills out indexed by (x<<8 | z<<4 | ly) for the 16x16x16
// subchunk subIdx (-64+subIdx*16 .. +15) using bit-exact hoisted cell lerps.
func (c *OverworldChunk) FillSubChunkDensities(subIdx int, out *[4096]float32) {
	yCellBase := subIdx * 2
	for cx := range 4 {
		for cz := range 4 {
			col00 := &c.cells[cx][cz]
			col10 := &c.cells[cx+1][cz]
			col01 := &c.cells[cx][cz+1]
			col11 := &c.cells[cx+1][cz+1]
			for halfY := range 2 {
				yCell := yCellBase + halfY
				a, b := &col00[yCell], &col00[yCell+1]
				cc, d := &col10[yCell], &col10[yCell+1]
				e, f := &col01[yCell], &col01[yCell+1]
				g, h := &col11[yCell], &col11[yCell+1]
				baseIdx := (cx*4)<<8 | (cz*4)<<4 | (halfY * 8)
				if a[cellNoodle] < noodleCellActiveThreshold && b[cellNoodle] < noodleCellActiveThreshold &&
					cc[cellNoodle] < noodleCellActiveThreshold && d[cellNoodle] < noodleCellActiveThreshold &&
					e[cellNoodle] < noodleCellActiveThreshold && f[cellNoodle] < noodleCellActiveThreshold &&
					g[cellNoodle] < noodleCellActiveThreshold && h[cellNoodle] < noodleCellActiveThreshold {
					fillDensityCellFast(a, b, cc, d, e, f, g, h, out, baseIdx)
				} else {
					fillNoodleCell(a, b, cc, d, e, f, g, h, out, baseIdx)
				}
			}
		}
	}
}

func fillDensityCellFast(a, b, c, d, e, f, g, h *overworldDensityCell, out *[4096]float32, baseIdx int) {
	a0, b0, c0, d0 := a[cellDensity], b[cellDensity], c[cellDensity], d[cellDensity]
	e0, f0, g0, h0 := e[cellDensity], f[cellDensity], g[cellDensity], h[cellDensity]
	for ly := range 8 {
		dy := float32(ly) * 0.125
		lAB := terrainLerp(dy, a0, b0)
		lCD := terrainLerp(dy, c0, d0)
		lEF := terrainLerp(dy, e0, f0)
		lGH := terrainLerp(dy, g0, h0)
		for lx := range 4 {
			dx := float32(lx) * 0.25
			low := terrainLerp(dx, lAB, lCD)
			high := terrainLerp(dx, lEF, lGH)
			rowIdx := baseIdx | (lx << 8) | ly
			out[rowIdx] = finalDensityFromRaw(terrainLerp(0, low, high))
			out[rowIdx|16] = finalDensityFromRaw(terrainLerp(0.25, low, high))
			out[rowIdx|32] = finalDensityFromRaw(terrainLerp(0.5, low, high))
			out[rowIdx|48] = finalDensityFromRaw(terrainLerp(0.75, low, high))
		}
	}
}

func fillNoodleCell(a, b, c, d, e, f, g, h *overworldDensityCell, out *[4096]float32, baseIdx int) {
	for ly := range 8 {
		dy := float32(ly) * 0.125
		var lAB, lCD, lEF, lGH [5]float32
		for field := range 5 {
			lAB[field] = terrainLerp(dy, a[field], b[field])
			lCD[field] = terrainLerp(dy, c[field], d[field])
			lEF[field] = terrainLerp(dy, e[field], f[field])
			lGH[field] = terrainLerp(dy, g[field], h[field])
		}
		for lx := range 4 {
			dx := float32(lx) * 0.25
			var low, high [5]float32
			for field := range 5 {
				low[field] = terrainLerp(dx, lAB[field], lCD[field])
				high[field] = terrainLerp(dx, lEF[field], lGH[field])
			}
			for lz := range 4 {
				dz := float32(lz) * 0.25
				density := finalDensityFromRaw(terrainLerp(dz, low[cellDensity], high[cellDensity]))
				if terrainLerp(dz, low[cellNoodle], high[cellNoodle]) >= 0 {
					thick := terrainLerp(dz, low[cellNoodleThickness], high[cellNoodleThickness])
					nA := terrainLerp(dz, low[cellNoodleA], high[cellNoodleA])
					nB := terrainLerp(dz, low[cellNoodleB], high[cellNoodleB])
					noodle := thick + 1.5*max(terrainAbs(nA), terrainAbs(nB))
					density = min(density, noodle)
				}
				out[baseIdx|(lx<<8)|(lz<<4)|ly] = density
			}
		}
	}
}

// Sample returns the interpolated final density. The caller adds structure
// beardification before consulting the aquifer.
func (c *OverworldChunk) Sample(localX, y, localZ int32) float32 {
	xCell, yCell, zCell := localX>>2, (y+64)>>3, localZ>>2
	return interpolateOverworldCells(&c.cells[xCell][zCell][yCell], &c.cells[xCell][zCell][yCell+1],
		&c.cells[xCell+1][zCell][yCell], &c.cells[xCell+1][zCell][yCell+1],
		&c.cells[xCell][zCell+1][yCell], &c.cells[xCell][zCell+1][yCell+1],
		&c.cells[xCell+1][zCell+1][yCell], &c.cells[xCell+1][zCell+1][yCell+1],
		float32(localX&3)*0.25, float32(y&7)*0.125, float32(localZ&3)*0.25)
}

func interpolateFieldPair(a, b, c, d, e, f, g, h *overworldDensityCell, field int, dx, dy, dz float32) float32 {
	low := terrainLerp(dx, terrainLerp(dy, a[field], b[field]), terrainLerp(dy, c[field], d[field]))
	high := terrainLerp(dx, terrainLerp(dy, e[field], f[field]), terrainLerp(dy, g[field], h[field]))
	return terrainLerp(dz, low, high)
}

func interpolateOverworldCells(a, b, c, d, e, f, g, h *overworldDensityCell, dx, dy, dz float32) float32 {
	density := finalDensityFromRaw(interpolateFieldPair(a, b, c, d, e, f, g, h, cellDensity, dx, dy, dz))
	if a[cellNoodle] >= noodleCellActiveThreshold || b[cellNoodle] >= noodleCellActiveThreshold ||
		c[cellNoodle] >= noodleCellActiveThreshold || d[cellNoodle] >= noodleCellActiveThreshold ||
		e[cellNoodle] >= noodleCellActiveThreshold || f[cellNoodle] >= noodleCellActiveThreshold ||
		g[cellNoodle] >= noodleCellActiveThreshold || h[cellNoodle] >= noodleCellActiveThreshold {
		if interpolateFieldPair(a, b, c, d, e, f, g, h, cellNoodle, dx, dy, dz) >= 0 {
			thick := interpolateFieldPair(a, b, c, d, e, f, g, h, cellNoodleThickness, dx, dy, dz)
			nA := interpolateFieldPair(a, b, c, d, e, f, g, h, cellNoodleA, dx, dy, dz)
			nB := interpolateFieldPair(a, b, c, d, e, f, g, h, cellNoodleB, dx, dy, dz)
			noodle := thick + 1.5*max(terrainAbs(nA), terrainAbs(nB))
			density = min(density, noodle)
		}
	}
	return density
}

// NoiseBiomeIndex reads the quart biome stored in a Java chunk, before Voronoi.
func (c *OverworldChunk) NoiseBiomeIndex(localX, y, localZ int32) uint8 {
	return c.biomes[(localX>>2)+1][(localZ>>2)+1][(max(-64, min(y, 319))>>2)+17].index
}

// BiomeAt implements BiomeManager.getBiome for positions inside this chunk.
func (c *OverworldChunk) BiomeAt(x, y, z int32) string {
	return overworldBiomeNames[c.BiomeIndexAt(x, y, z)]
}

// BiomeIndexAt is the numeric BiomeManager result, avoiding name lookups when
// filling the Bedrock biome palette.
func (c *OverworldChunk) BiomeIndexAt(x, y, z int32) uint8 {
	localX, localZ := x-c.baseX, z-c.baseZ
	if localX < 0 || localX >= 16 || localZ < 0 || localZ >= 16 || y < -64 || y >= 320 {
		qx, qy, qz := FiddledBiomeQuart(c.zoomSeed, x, y, z)
		column := c.noise.SampleBiomeColumn(qx*4, qz*4)
		blockY := max(-16, min(qy, 79)) * 4
		return findOverworldBiomeIndex([6]float32{column.Temperature, column.Vegetation, column.Continentalness, column.Erosion, column.Depth(blockY), column.Weirdness}, nil)
	}
	ax, ay, az := localX-2, y-2, localZ-2
	px, py, pz := (ax>>2)+1, (ay>>2)+17, (az>>2)+1
	// Most cells lie inside one biome: the nearest fiddled corner then does
	// not matter.
	first := c.biomes[px][pz][py].index
	if c.biomes[px][pz][py+1].index == first && c.biomes[px][pz+1][py].index == first && c.biomes[px][pz+1][py+1].index == first &&
		c.biomes[px+1][pz][py].index == first && c.biomes[px+1][pz][py+1].index == first && c.biomes[px+1][pz+1][py].index == first && c.biomes[px+1][pz+1][py+1].index == first {
		return first
	}
	fx, fy, fz := float64(ax&3)*0.25, float64(ay&3)*0.25, float64(az&3)*0.25
	best, distance := uint8(0), math.Inf(1)
	for corner := range 8 {
		dx, dy, dz := int32(corner>>2), int32((corner>>1)&1), int32(corner&1)
		cell := &c.biomes[px+dx][pz+dz][py+dy]
		if !cell.fiddleValid {
			cell.fiddle = biomeFiddleAt(c.zoomSeed, (c.baseX>>2)+(px+dx-1), (py+dy)-17, (c.baseZ>>2)+(pz+dz-1))
			cell.fiddleValid = true
		}
		next := biomeFiddleDistance(fx-float64(dx), fy-float64(dy), fz-float64(dz), cell.fiddle)
		if next < distance {
			best, distance = cell.index, next
		}
	}
	return best
}

// PreliminarySurfaceHeight is MaterialSystem's floored 16-block interpolation,
// distinct from the raw FindTopSurface samples used by aquifer surface probes.
func (c *OverworldChunk) PreliminarySurfaceHeight(x, z int32) int32 {
	dx, dz := float32(x-c.baseX)/16, float32(z-c.baseZ)/16
	low := terrainLerp(dx, c.preliminary[0][0], c.preliminary[1][0])
	high := terrainLerp(dx, c.preliminary[0][1], c.preliminary[1][1])
	return int32(math.Floor(float64(terrainLerp(dz, low, high))))
}
