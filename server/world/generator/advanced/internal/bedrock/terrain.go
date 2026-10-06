package bedrock

// TerrainShape samples the height-independent Overworld terrain controls.
// The shared climate must have been initialised with the same world seed.
type TerrainShape struct {
	climate *Climate
	jagged  NormalNoise
}

// TerrainColumn is worker-owned terrain scratch. Factor uses native density
// units, which are 128 times the corresponding Java density-function units.
type TerrainColumn struct {
	Offset, Factor, Jaggedness float32
}

func NewTerrainShape(seed int64, climate *Climate) *TerrainShape {
	return &TerrainShape{
		climate: climate,
		jagged:  NewRandomFactory(seed).Noise("minecraft:jagged", -16, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1),
	}
}

// SampleColumn samples block coordinates rounded down to the four-block grid.
func (s *TerrainShape) SampleColumn(x, z int32, column *TerrainColumn) {
	_, _, continents, erosion, weirdness := s.climate.terrainAt(x, z)
	column.Offset = float32(TerrainOffset(continents, erosion, weirdness) - 0.50375)
	column.Factor = float32(TerrainFactor(continents, erosion, weirdness) * 128)
	column.Jaggedness = 0
	shape := TerrainJaggedness(continents, erosion, weirdness)
	if shape > 0 {
		noise := s.jagged.Sample(float32(x&^3)*1500, 0, float32(z&^3)*1500)
		if !(noise > 0) {
			shape = float32(shape * 0.5)
		}
		column.Jaggedness = float32(shape * noise)
	}
}

// SurfaceHeight returns the native preliminary surface used by aquifers. It
// excludes base 3D noise, jaggedness, caves and decoration. The value 32767 is
// the native sentinel when no eight-block sample exceeds the density threshold.
func (s *TerrainShape) SurfaceHeight(x, z int32) int32 {
	_, _, continents, erosion, weirdness := s.climate.terrainAt(x, z)
	offset := float32(TerrainOffset(continents, erosion, weirdness) - 0.50375)
	factor := float32(TerrainFactor(continents, erosion, weirdness) * 128)
	for gridY := int32(39); gridY >= 0; gridY-- {
		y := gridY*8 - 64
		depth := float32(float32(float32(y)*-0.0078125) + 1)
		density := float32(float32(depth+offset) * factor)
		if density > 0 {
			density = float32(density * 4)
		}
		density = float32(density - 90)
		top := float32(float32(40-gridY) * 0.5)
		if top <= 1 {
			density = float32(float32(top*float32(density+10)) - 10)
		}
		bottom := float32(gridY) / 3
		if bottom <= 1 {
			density = float32(float32(float32(density-15)*bottom) + 15)
		}
		if density > 50 {
			return y
		}
	}
	return 32767
}
