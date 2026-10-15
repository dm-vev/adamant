package java26

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
	fiddle [3]float64
	index  uint8
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
			column := noise.SampleColumn(wx, wz)
			c.columns[x][z] = column
			for y := range 98 {
				quartY := int32(y - 17)
				clampedY := max(-16, min(quartY, 79))
				var index uint8
				if biomes != nil {
					index = biomes(wx>>2, clampedY, wz>>2)
				} else {
					index = findOverworldBiomeIndex([6]float32{column.Temperature, column.Vegetation, column.Continentalness, column.Erosion, column.Depth(clampedY * 4), column.Weirdness}, &hint)
				}
				c.biomes[x][z][y] = overworldBiomeCell{index: index, fiddle: biomeFiddleAt(zoomSeed, wx>>2, quartY, wz>>2)}
			}
			if x == 0 || z == 0 {
				continue
			}
			for y := range 49 {
				c.cells[x-1][z-1][y] = noise.sampleDensityCell(wx, int32(y)*8-64, wz, column, true)
			}
		}
	}
	for x := range 2 {
		for z := range 2 {
			c.preliminary[x][z] = float32(preliminarySurfaceHeight(c.columns[1+x*4][1+z*4]))
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

func interpolateOverworldCells(a, b, c, d, e, f, g, h *overworldDensityCell, dx, dy, dz float32) float32 {
	var interpolated [5]float32
	for field := range 5 {
		low := terrainLerp(dx, terrainLerp(dy, a[field], b[field]), terrainLerp(dy, c[field], d[field]))
		high := terrainLerp(dx, terrainLerp(dy, e[field], f[field]), terrainLerp(dy, g[field], h[field]))
		interpolated[field] = terrainLerp(dz, low, high)
	}
	value := clampDensity(interpolated[cellDensity], -1, 1)
	density := value*0.5 - value*value*value/24
	if interpolated[cellNoodle] >= 0 {
		noodle := interpolated[cellNoodleThickness] + 1.5*max(terrainAbs(interpolated[cellNoodleA]), terrainAbs(interpolated[cellNoodleB]))
		density = min(density, noodle)
	}
	return density
}

// NoiseBiomeIndex reads the quart biome stored in a Java chunk, before Voronoi.
func (c *OverworldChunk) NoiseBiomeIndex(localX, y, localZ int32) uint8 {
	return c.biomes[(localX>>2)+1][(localZ>>2)+1][(max(-64, min(y, 319))>>2)+17].index
}

// JavaBiomeAt implements BiomeManager.getBiome for positions inside this chunk.
func (c *OverworldChunk) JavaBiomeAt(x, y, z int32) string {
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
	fx, fy, fz := float64(ax&3)*0.25, float64(ay&3)*0.25, float64(az&3)*0.25
	best, distance := uint8(0), math.Inf(1)
	for corner := range 8 {
		dx, dy, dz := int32(corner>>2), int32((corner>>1)&1), int32(corner&1)
		cell := &c.biomes[px+dx][pz+dz][py+dy]
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
