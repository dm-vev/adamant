package bedrock

const (
	densityColumnSize = 41
	densityGridSize   = 5 * 5 * densityColumnSize
)

const (
	densityTerrain = iota
	densityNoodle
	densityNoodleThickness
	densityNoodleA
	densityNoodleB
	densityVein
	densityVeinA
	densityVeinB
	densityFields
)

// TerrainDensity composes the unblended modern Overworld density graph. The
// shared shape must use the same seed. All mutable scratch belongs to workers.
type TerrainDensity struct {
	shape     *TerrainShape
	blended   *BlendedNoise
	caves     *CaveDensity
	auxiliary [densityFields - 1]NormalNoise
}

func NewTerrainDensity(seed int64, shape *TerrainShape) *TerrainDensity {
	factory := NewRandomFactory(seed)
	return &TerrainDensity{
		shape: shape, blended: NewBlendedNoise(seed), caves: NewCaveDensity(seed),
		auxiliary: [densityFields - 1]NormalNoise{
			factory.Noise("minecraft:noodle", -8, 1),
			factory.Noise("minecraft:noodle_thickness", -8, 1),
			factory.Noise("minecraft:noodle_ridge_a", -7, 1),
			factory.Noise("minecraft:noodle_ridge_b", -7, 1),
			factory.Noise("minecraft:ore_veininess", -8, 1),
			factory.Noise("minecraft:ore_vein_a", -7, 1),
			factory.Noise("minecraft:ore_vein_b", -7, 1),
		},
	}
}

// TerrainGrid stores the 5×41×5 vertices spanning Y=-64..256. The native
// Overworld density stage leaves Y>=256 empty; surface features run separately.
type TerrainGrid struct {
	values [densityFields][densityGridSize]float32
}

func (density *TerrainDensity) Prepare(chunkX, chunkZ int32, grid *TerrainGrid) {
	density.blended.Fill(chunkX, chunkZ, &grid.values[densityTerrain])
	for x := range 5 {
		blockX := chunkX*16 + int32(x)*4
		for z := range 5 {
			blockZ := chunkZ*16 + int32(z)*4
			var column TerrainColumn
			density.shape.SampleColumn(blockX, blockZ, &column)
			base := (x*5 + z) * densityColumnSize
			for y := range densityColumnSize {
				blockY := int32(y)*8 - 64
				depth := float32(float32(float32(blockY)*-0.0078125) + 1)
				slope := float32(float32(float32(depth+column.Offset)+column.Jaggedness) * column.Factor)
				if slope > 0 {
					slope = float32(slope * 4)
				}
				index := base + y
				value := float32(slope + grid.values[densityTerrain][index])
				value = density.caves.Sample(blockX, blockY, blockZ, value)
				top := float32(40-y) * 0.5
				if top <= 1 {
					value = float32(float32(float32(value+10)*top) - 10)
				}
				bottom := float32(y) / 3
				if bottom <= 1 {
					value = float32(float32(float32(value-15)*bottom) + 15)
				}
				grid.values[densityTerrain][index] = value
				if value >= -150 {
					density.sampleAuxiliary(blockX, blockY, blockZ, index, grid)
				} else {
					// Skipped native auxiliary vertices retain their initial
					// value, including when this worker reuses its grid.
					for field := range densityFields - 1 {
						grid.values[field+1][index] = 1
					}
				}
			}
		}
	}
}

func (density *TerrainDensity) sampleAuxiliary(x, y, z int32, index int, grid *TerrainGrid) {
	px, py, pz := float32(x), float32(y), float32(z)
	for field := range densityFields - 1 {
		sx, sy, sz := px, py, pz
		switch field + 1 {
		case densityNoodleA, densityNoodleB:
			sx, sy, sz = float32(px*2.6666667), float32(py*2.6666667), float32(pz*2.6666667)
		case densityVein:
			sx, sy, sz = float32(px*1.5), float32(py*1.5), float32(pz*1.5)
		case densityVeinA, densityVeinB:
			sx, sy, sz = float32(px*4), float32(py*4), float32(pz*4)
		}
		grid.values[field+1][index] = clampDensityNoise(density.auxiliary[field].Sample(sx, sy, sz))
	}
}

// clampDensityNoise also reproduces the native unordered comparison result.
func clampDensityNoise(value float32) float32 {
	if value >= -1 {
		if value > 1 {
			return 1
		}
		return value
	}
	return -1
}

// TerrainCell caches horizontal interpolation for a 4×8×4 cell. Select a cell,
// then SetZ and SetX in that order before sampling its eight Y positions.
type TerrainCell struct {
	grid   *TerrainGrid
	base   int
	minY   int16
	zLines [densityFields][4]float32
	xLines [densityFields][2]float32
}

// PrepareCell selects cell X/Z in [0,3] and Y in [0,39] without copying vertices.
func (grid *TerrainGrid) PrepareCell(x, y, z uint8, cell *TerrainCell) {
	cell.grid = grid
	cell.base = (int(x)*5+int(z))*densityColumnSize + int(y)
	cell.minY = int16(y)*8 - 64
}

// SetZ caches the native first interpolation axis at a local offset in [0,3].
func (cell *TerrainCell) SetZ(z uint8) {
	fraction := float32(z) * 0.25
	base := cell.base
	for field := range densityFields {
		values := &cell.grid.values[field]
		line := &cell.zLines[field]
		line[0] = lerp32(fraction, values[base], values[base+densityColumnSize])
		line[1] = lerp32(fraction, values[base+5*densityColumnSize], values[base+6*densityColumnSize])
		line[2] = lerp32(fraction, values[base+1], values[base+densityColumnSize+1])
		line[3] = lerp32(fraction, values[base+5*densityColumnSize+1], values[base+6*densityColumnSize+1])
	}
}

// SetX caches the native second interpolation axis at a local offset in [0,3].
func (cell *TerrainCell) SetX(x uint8) {
	fraction := float32(x) * 0.25
	for field := range densityFields {
		line := &cell.zLines[field]
		cell.xLines[field][0] = lerp32(fraction, line[0], line[1])
		cell.xLines[field][1] = lerp32(fraction, line[2], line[3])
	}
}

func (cell *TerrainCell) value(field int, y uint8) float32 {
	line := &cell.xLines[field]
	return float32(float32(float32(float32(line[1]-line[0])*0.125)*float32(y)) + line[0])
}

// Density applies the native squeeze and noodle caves at a local Y in [0,7].
// Aquifer pressure and structure density adjustments are applied by the caller.
func (cell *TerrainCell) Density(y uint8) float32 {
	value := float32(cell.value(densityTerrain, y) * 0.005)
	if value <= -1 {
		value = -1
	}
	if value > 1 {
		value = 1
	}
	value = float32(float32(float32(float32(value*value)*value)/-24) + float32(value*0.5))
	if cell.minY+int16(y) >= -60 && value > 0 && cell.value(densityNoodle, y) >= 0 {
		ratio := float32(float32(cell.value(densityNoodleThickness, y)+1) * 0.5)
		thickness := float32(0.05)
		if ratio >= 0 {
			thickness = 0.1
			if ratio <= 1 {
				thickness = float32(float32(ratio*0.05) + 0.05)
			}
		}
		a := float32(caveAbs(float32(cell.value(densityNoodleA, y)*1.5)) - thickness)
		b := float32(caveAbs(float32(cell.value(densityNoodleB, y)*1.5)) - thickness)
		if b <= a {
			b = a
		}
		if value <= b {
			b = value
		}
		value = b
	}
	return value
}

// VeinNoise samples the ore selector and two ridge values at a local Y in [0,7].
// Call only for solid voxels within the ore types' vertical ranges.
func (cell *TerrainCell) VeinNoise(y uint8) (selector, ridgeA, ridgeB float32) {
	return cell.value(densityVein, y), cell.value(densityVeinA, y), cell.value(densityVeinB, y)
}
