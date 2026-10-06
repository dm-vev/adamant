package bedrock

// Climate samples the modern Overworld's six climate coordinates. Its noise
// state is immutable and can be shared by chunk-generation workers.
type Climate struct {
	temperature, vegetation, continentalness, erosion, ridge, shift NormalNoise
}

// ClimateColumn caches the five height-independent coordinates of one climate
// column. Each worker owns its scratch columns; PointAt mutates their depth.
type ClimateColumn struct {
	point       ClimatePoint
	depthOffset float32
}

func NewClimate(seed int64) *Climate {
	factory := NewRandomFactory(seed)
	return &Climate{
		temperature:     factory.Noise("minecraft:temperature", -10, 1.5, 0, 1, 0, 0, 0),
		vegetation:      factory.Noise("minecraft:vegetation", -8, 1, 1, 0, 0, 0, 0),
		continentalness: factory.Noise("minecraft:continentalness", -9, 1, 1, 2, 2, 2, 1, 1, 1, 1),
		erosion:         factory.Noise("minecraft:erosion", -9, 1, 1, 0, 1, 1),
		ridge:           factory.Noise("minecraft:ridge", -7, 1, 2, 1, 0, 0, 0),
		shift:           factory.Noise("minecraft:offset", -3, 1, 1, 1, 0),
	}
}

// SampleColumn samples at block coordinates, rounded down to the four-block
// noise grid. Biome-cell centres are spaced eight blocks apart. Sampling once
// per column avoids repeating the same noise and spline work at every height.
func (c *Climate) SampleColumn(x, z int32, column *ClimateColumn) {
	qx, qz := float32(x>>2), float32(z>>2)
	px := float32(qx + float32(c.shift.Sample(qx, 0, qz)*4))
	pz := float32(qz + float32(c.shift.Sample(qz, qx, 0)*4))
	continents := c.continentalness.Sample(px, 0, pz)
	erosion := c.erosion.Sample(px, 0, pz)
	weirdness := c.ridge.Sample(px, 0, pz)
	column.point[0] = quantizeClimate(c.temperature.Sample(px, 0, pz))
	column.point[1] = quantizeClimate(c.vegetation.Sample(px, 0, pz))
	column.point[2] = quantizeClimate(continents)
	column.point[3] = quantizeClimate(erosion)
	column.point[5] = quantizeClimate(weirdness)
	// BDS stores this as the single float32 constant 0xbf00f5c3, rather
	// than adding 0.015 and subtracting 83/160 in separate operations.
	column.depthOffset = float32(TerrainOffset(continents, erosion, weirdness) - 0.50375)
}

// PointAt returns this column's climate point at the block height. The pointer
// belongs to the column and remains valid only until its next sampling call.
func (c *ClimateColumn) PointAt(y int32) *ClimatePoint {
	gradient := float32(float32(float32(y)*-0.0078125) + 1)
	c.point[4] = quantizeClimate(float32(gradient + c.depthOffset))
	return &c.point
}

func quantizeClimate(value float32) int64 {
	// Bedrock widens before multiplying; multiplying in float32 changes
	// interval boundaries and can select a different biome.
	return int64(float64(value) * 10000)
}
