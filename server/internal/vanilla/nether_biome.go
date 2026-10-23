package vanilla

// NetherBiomes implements the two legacy-Nether biome noises and the five
// points in MultiNoiseBiomeSourceParameterList.Preset.NETHER (MC 26.3).
type NetherBiomes struct {
	temperature *NormalNoise
	vegetation  *NormalNoise
}

func NewNetherBiomes(seed int64) *NetherBiomes {
	temperatureRandom := NewLegacyRandom(seed)
	vegetationRandom := NewLegacyRandom(seed + 1)
	return &NetherBiomes{
		temperature: NewLegacyNetherNoise(&temperatureRandom, -7, 1, 1),
		vegetation:  NewLegacyNetherNoise(&vegetationRandom, -7, 1, 1),
	}
}

// Biome returns the vanilla resource name for a block-space coordinate.
func (b *NetherBiomes) Biome(x, z int32) string {
	temperature := int64(float32(b.temperature.Sample(float64(x)*0.25, 0, float64(z)*0.25)) * 10000)
	humidity := int64(float32(b.vegetation.Sample(float64(x)*0.25, 0, float64(z)*0.25)) * 10000)
	// All remaining Nether climate dimensions are zero. Each ParameterPoint
	// is the exact point declared by the vanilla Nether biome preset.
	points := [...]struct {
		name                         string
		temperature, humidity, weird int64
	}{
		{"minecraft:nether_wastes", 0, 0, 0},
		{"minecraft:soul_sand_valley", 0, -5000, 0},
		{"minecraft:crimson_forest", 4000, 0, 0},
		{"minecraft:warped_forest", 0, 5000, 3750},
		{"minecraft:basalt_deltas", -5000, 0, 1750},
	}
	best := int64(^uint64(0) >> 1)
	selected := points[0].name
	for _, point := range points {
		dt, dh, dw := temperature-point.temperature, humidity-point.humidity, point.weird
		distance := dt*dt + dh*dh + dw*dw
		if distance < best {
			best, selected = distance, point.name
		}
	}
	return selected
}
