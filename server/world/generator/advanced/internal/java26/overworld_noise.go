package java26

import "math"

// OverworldNoise is the immutable Java 26.3 NoiseRouterData Overworld graph.
// Caches and interpolation volumes belong to its caller, never to the sampler.
type OverworldNoise struct {
	Random                                                                        PositionalFactory
	shift, temperature, vegetation, continents, erosion, ridge, jagged            *NormalNoise
	blended                                                                       *BlendedNoise
	roughness, roughnessModulator, entrance, layer, cheese                        *NormalNoise
	spaghetti3D1, spaghetti3D2, spaghetti3DRarity, spaghetti3DThickness           *NormalNoise
	spaghetti2D, spaghetti2DElevation, spaghetti2DModulator, spaghetti2DThickness *NormalNoise
	pillar, pillarRareness, pillarThickness                                       *NormalNoise
	noodle, noodleThickness, noodleRidgeA, noodleRidgeB                           *NormalNoise
	veininess, veinA, veinB, veinGap                                              *NormalNoise
	barrier, floodedness, spread, lava                                            *NormalNoise
}

// NewOverworldNoise instantiates the normal Overworld's named Java random streams.
func NewOverworldNoise(seed int64) *OverworldNoise {
	random := NewXoroshiroRandom(seed)
	factory := random.ForkPositional()
	noise := func(name string, octave int, amplitudes ...float64) *NormalNoise {
		r := factory.FromHashOf("minecraft:" + name)
		return NewNormalNoise(&r, octave, amplitudes...)
	}
	base := factory.FromHashOf("minecraft:terrain")
	return &OverworldNoise{
		Random:               factory,
		shift:                noise("offset", -3, 1, 1, 1, 0),
		temperature:          noise("temperature", -10, 1.5, 0, 1, 0, 0, 0),
		vegetation:           noise("vegetation", -8, 1, 1, 0, 0, 0, 0),
		continents:           noise("continentalness", -9, 1, 1, 2, 2, 2, 1, 1, 1, 1),
		erosion:              noise("erosion", -9, 1, 1, 0, 1, 1),
		ridge:                noise("ridge", -7, 1, 2, 1, 0, 0, 0),
		jagged:               noise("jagged", -16, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1),
		blended:              NewBlendedNoise(&base, 0.25, 0.125, 80, 160, 8),
		roughness:            noise("spaghetti_roughness", -5, 1),
		roughnessModulator:   noise("spaghetti_roughness_modulator", -8, 1),
		entrance:             noise("cave_entrance", -7, 0.4, 0.5, 1),
		layer:                noise("cave_layer", -8, 1),
		cheese:               noise("cave_cheese", -8, 0.5, 1, 2, 1, 2, 1, 0, 2, 0),
		spaghetti3D1:         noise("spaghetti_3d_1", -7, 1),
		spaghetti3D2:         noise("spaghetti_3d_2", -7, 1),
		spaghetti3DRarity:    noise("spaghetti_3d_rarity", -11, 1),
		spaghetti3DThickness: noise("spaghetti_3d_thickness", -8, 1),
		spaghetti2D:          noise("spaghetti_2d", -7, 1),
		spaghetti2DElevation: noise("spaghetti_2d_elevation", -8, 1),
		spaghetti2DModulator: noise("spaghetti_2d_modulator", -11, 1),
		spaghetti2DThickness: noise("spaghetti_2d_thickness", -11, 1),
		pillar:               noise("pillar", -7, 1, 1),
		pillarRareness:       noise("pillar_rareness", -8, 1),
		pillarThickness:      noise("pillar_thickness", -8, 1),
		noodle:               noise("noodle", -8, 1),
		noodleThickness:      noise("noodle_thickness", -8, 1),
		noodleRidgeA:         noise("noodle_ridge_a", -7, 1),
		noodleRidgeB:         noise("noodle_ridge_b", -7, 1),
		veininess:            noise("ore_veininess", -8, 1),
		veinA:                noise("ore_vein_a", -7, 1),
		veinB:                noise("ore_vein_b", -7, 1),
		veinGap:              noise("ore_gap", -5, 1),
		barrier:              noise("aquifer_barrier", -3, 1),
		floodedness:          noise("aquifer_fluid_level_floodedness", -7, 1),
		spread:               noise("aquifer_fluid_level_spread", -5, 1),
		lava:                 noise("aquifer_lava", -1, 1),
	}
}

// OverworldColumn holds horizontally invariant climate and terrain inputs.
type OverworldColumn struct {
	Temperature, Vegetation, Continentalness, Erosion, Weirdness float32
	Offset, Factor, Jaggedness                                   float32
}

func (n *OverworldNoise) SampleColumn(x, z int32) OverworldColumn {
	column := n.SampleBiomeColumn(x, z)
	n.applyJaggedness(&column, x, z)
	return column
}

// SampleBiomeColumn skips jagged terrain noise, which does not affect climate.
func (n *OverworldNoise) SampleBiomeColumn(x, z int32) OverworldColumn {
	column, nx, nz := n.shapeColumn(x, z)
	column.Temperature = n.temperature.Sample(nx, 0, nz)
	column.Vegetation = n.vegetation.Sample(nx, 0, nz)
	return column
}

func (n *OverworldNoise) shapeColumn(x, z int32) (OverworldColumn, float64, float64) {
	sx, sz := float64(x)*0.25, float64(z)*0.25
	nx := sx + float64(n.shift.Sample(sx, 0, sz)*4)
	nz := sz + float64(n.shift.Sample(sz, sx, 0)*4)
	column := OverworldColumn{
		Continentalness: n.continents.Sample(nx, 0, nz),
		Erosion:         n.erosion.Sample(nx, 0, nz),
		Weirdness:       n.ridge.Sample(nx, 0, nz),
	}
	offset, factor, jaggedness := TerrainShape(column.Continentalness, column.Erosion, column.Weirdness)
	column.Offset, column.Factor, column.Jaggedness = offset-0.50375, factor, jaggedness
	return column, nx, nz
}

func (n *OverworldNoise) applyJaggedness(column *OverworldColumn, x, z int32) {
	if column.Jaggedness == 0 {
		return
	}
	jagged := n.jagged.Sample(float64(x)*1500, 0, float64(z)*1500)
	if jagged < 0 {
		jagged *= 0.5
	}
	column.Jaggedness *= jagged
}

func (c OverworldColumn) Depth(y int32) float32 {
	return gradientY(y, -64, 320, 1.5, -1.5) + c.Offset
}

func (c OverworldColumn) Biome(y int32) string {
	return FindOverworldBiome([6]float32{c.Temperature, c.Vegetation, c.Continentalness, c.Erosion, c.Depth(y), c.Weirdness})
}

func (n *OverworldNoise) PreliminarySurfaceHeight(x, z int32) int32 {
	column, _, _ := n.shapeColumn(x, z)
	return preliminarySurfaceHeight(column)
}

func preliminarySurfaceHeight(column OverworldColumn) int32 {
	upper := (float32(0.2734375)/column.Factor-column.Offset)*-128 + 128
	top := int32(math.Floor(float64(clampDensity(upper, -40, 320))/8)) * 8
	for y := top; y >= -64; y -= 8 {
		density := clampDensity(gradientDensity(column.Factor, column.Depth(y))-0.703125, -64, 64)
		if slideOverworld(y, density)-0.390625 > 0 {
			return y
		}
	}
	return -64
}

func (n *OverworldNoise) rawDensity(x, y, z int32, column OverworldColumn) float32 {
	if y >= 256 {
		return -0.078125 * 0.64
	}
	if y <= -64 {
		return 0.1171875 * 0.64
	}
	sloped := gradientDensity(column.Factor, column.Depth(y)+column.Jaggedness) + n.blended.Sample(x, y, z)
	nx, ny, nz := float64(x), float64(y), float64(z)
	roughness := (terrainAbs(n.roughness.Sample(nx, ny, nz)) - 0.4) * mapNoise(n.roughnessModulator.Sample(nx, ny, nz), 0, -0.1)
	rarity := rarity3D(n.spaghetti3DRarity.Sample(nx*2, ny, nz*2))
	dx, dy, dz := nx/float64(rarity), ny/float64(rarity), nz/float64(rarity)
	spaghetti := max(terrainAbs(n.spaghetti3D1.Sample(dx, dy, dz)), terrainAbs(n.spaghetti3D2.Sample(dx, dy, dz))) * rarity
	spaghetti = clampDensity(spaghetti+mapNoise(n.spaghetti3DThickness.Sample(nx, ny, nz), -0.065, -0.088), -1, 1)
	entrances := min(n.entrance.Sample(nx*0.75, ny*0.5, nz*0.75)+0.37+gradientY(y, -10, 30, 0.3, 0), roughness+spaghetti)
	if sloped < 1.5625 {
		return slideOverworld(y, min(sloped, entrances*5)) * 0.64
	}
	layer := n.layer.Sample(nx, ny*8, nz)
	cheese := clampDensity(n.cheese.Sample(nx, ny*(2.0/3.0), nz)+0.27, -1, 1) + clampDensity(sloped*-0.64+1.5, 0, 0.5)
	thickness := mapNoise(n.spaghetti2DThickness.Sample(nx*2, ny, nz*2), -0.6, -1.3)
	elevation := mapNoise(n.spaghetti2DElevation.Sample(nx, 0, nz), -8, 8)
	ridge := terrainAbs(elevation+gradientY(y, -64, 320, 8, -40)) + thickness
	rarity = rarity2D(n.spaghetti2DModulator.Sample(nx*2, ny, nz*2))
	cave := terrainAbs(n.spaghetti2D.Sample(nx/float64(rarity), ny/float64(rarity), nz/float64(rarity)))*rarity + thickness*0.083
	spaghetti = clampDensity(max(cave, ridge*ridge*ridge), -1, 1)
	pillarThickness := mapNoise(n.pillarThickness.Sample(nx, ny, nz), 0, 1.1)
	pillar := (n.pillar.Sample(nx*25, ny*0.3, nz*25)*2 + mapNoise(n.pillarRareness.Sample(nx, ny, nz), 0, -2)) * pillarThickness * pillarThickness * pillarThickness
	if pillar < 0.03 {
		pillar = -1000000
	}
	return slideOverworld(y, max(min(layer*layer*4+cheese, entrances, spaghetti+roughness), pillar)) * 0.64
}

func rarity3D(value float32) float32 {
	switch {
	case value < -0.5:
		return 0.75
	case value < 0:
		return 1
	case value < 0.5:
		return 1.5
	default:
		return 2
	}
}

func rarity2D(value float32) float32 {
	switch {
	case value < -0.75:
		return 0.5
	case value < -0.5:
		return 0.75
	case value < 0.5:
		return 1
	case value < 0.75:
		return 2
	default:
		return 3
	}
}

func gradientDensity(factor, depth float32) float32 {
	value := depth * factor
	if value > 0 {
		value *= 4
	}
	return value
}

func gradientY(y, low, high int32, start, end float32) float32 {
	return terrainLerp(clampDensity(float32(y-low)/float32(high-low), 0, 1), start, end)
}

func slideOverworld(y int32, value float32) float32 {
	value = terrainLerp(gradientY(y, 240, 256, 1, 0), -0.078125, value)
	return terrainLerp(gradientY(y, -64, -40, 0, 1), 0.1171875, value)
}

func mapNoise(value, minimum, maximum float32) float32 {
	factor := (maximum - minimum) / 2
	return value*factor + (minimum + factor)
}

func clampDensity(value, minimum, maximum float32) float32 {
	return min(max(value, minimum), maximum)
}
